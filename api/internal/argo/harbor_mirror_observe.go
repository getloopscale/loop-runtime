// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T3.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure-plan.md.
// Per ADR D2 + D5 + v1.1 EC-NEW-7 + EC-NEW-8.
//
// Observability for Harbor mirror failures (which run with
// continueOn.failed=true → silent without this). Triggered via Argo
// Workflows lifecycle hook on mirror-harbor-{app} task when status=Failed.
//
// Emits:
//   - Loki structured JSON log (curl POST with --max-time 10)
//   - Prometheus counter via Pushgateway with Push-Lifetime: 24h header
//     (ADR D5: TTL prevents stale metric pollution)
//
// Failure semantics:
//   - Loki/Pushgateway unreachable → log to stderr + exit 0 (best-effort)
//   - Workflow path unaffected (preserves continueOn semantics)

package argo

import (
	"fmt"

	forge "github.com/getloopscale/loopzero-forge"
	"github.com/getloopscale/loopzero-forge/model"
)

// harborMirrorObservabilityHook builds the LifecycleHook attached to
// mirror-harbor-{app} task. Triggered when task status=Failed (Argo
// expression syntax). Passes app + tenant + error to the
// harbor-mirror-observe template.
//
// Expression `tasks.{name}.status == "Failed"` ensures hook fires ONLY
// on failure (not Succeeded/Cancelled) — prevents spurious WARN logs
// per v1.1 EC-15 DOCUMENT clarification.
func harborMirrorObservabilityHook(mirrorTaskName, appName, tenantID, harborEndpoint string) model.LifecycleHook {
	return model.LifecycleHook{
		Template:   "harbor-mirror-observe",
		Expression: fmt.Sprintf(`tasks["%s"].status == "Failed"`, mirrorTaskName),
		Arguments: &model.ArgumentsModel{
			Parameters: []model.ParameterModel{
				{Name: "app-name", Value: forge.Ptr(appName)},
				{Name: "tenant-id", Value: forge.Ptr(tenantID)},
				{Name: "workflow-phase", Value: forge.Ptr("{{workflow.status}}")},
				{Name: "harbor-endpoint", Value: forge.Ptr(harborEndpoint)},
				{Name: "error-msg", Value: forge.Ptr(fmt.Sprintf(`{{tasks["%s"].outputs.result}}`, mirrorTaskName))},
			},
		},
	}
}

// harborMirrorObserveTemplate produces the exit-handler template invoked
// when a mirror-harbor-{app} step fails. Single shared template (NOT
// per-app) — receives app name + tenant via parameters from caller.
func harborMirrorObserveTemplate() *forge.Script {
	script := `set -eu

# Configuration — Argo Workflow injects via env from parameters
APP_NAME="${APP_NAME:?missing APP_NAME}"
TENANT_ID="${TENANT_ID:?missing TENANT_ID}"
WORKFLOW_PHASE="${WORKFLOW_PHASE:-unknown}"
HARBOR_ENDPOINT="${HARBOR_ENDPOINT:-unknown}"
ERROR_MSG="${ERROR_MSG:-mirror step failed}"

LOKI_URL="${LOKI_PUSH_URL:-http://loki.monitoring.svc.cluster.local:3100/loki/api/v1/push}"
PUSHGW_URL="${PUSHGATEWAY_URL:-http://prometheus-pushgateway.monitoring.svc.cluster.local:9091}"

NOW_NS=$(date +%s%N)

# 1. Loki structured log
# v1.1 EC-NEW-8: --max-time 10 --connect-timeout 5 prevents hang on Loki outage
# (without this, exit handler hangs up to ~120s → Workflow stuck Cancelling).
# Use printf to safely interpolate variables into the JSON payload (avoid
# heredoc-quoting pitfalls when ERROR_MSG contains quotes/newlines).
LOKI_PAYLOAD=$(printf '{"streams":[{"stream":{"level":"warn","component":"build-pipeline","event":"harbor_mirror_failed","app":"%s","tenant":"%s","workflow_phase":"%s","harbor_endpoint":"%s"},"values":[["%s","%s"]]}]}' \
  "$APP_NAME" "$TENANT_ID" "$WORKFLOW_PHASE" "$HARBOR_ENDPOINT" "$NOW_NS" "$ERROR_MSG")

if ! curl --max-time 10 --connect-timeout 5 \
    -s -X POST \
    -H "Content-Type: application/json" \
    -d "$LOKI_PAYLOAD" \
    "$LOKI_URL" >/dev/null 2>&1; then
  echo "[harbor-mirror-observe] WARN: Loki POST failed (non-fatal); event lost to Loki but Prometheus push next." >&2
fi

# 2. Pushgateway counter with TTL header (ADR D5)
# v1.1 EC-NEW-7: Push-Lifetime: 24h auto-expires entry (no stale metric).
# v1.1 EC-NEW-8: --max-time 10 --connect-timeout 5 prevents hang.
PUSHGW_PAYLOAD="# HELP loopzero_harbor_mirror_failure_total Count of harbor mirror step failures
# TYPE loopzero_harbor_mirror_failure_total counter
loopzero_harbor_mirror_failure_total{app=\"$APP_NAME\",tenant=\"$TENANT_ID\",harbor_endpoint=\"$HARBOR_ENDPOINT\"} 1
"

if ! echo "$PUSHGW_PAYLOAD" | curl --max-time 10 --connect-timeout 5 \
    -s -X POST \
    -H "Content-Type: text/plain" \
    -H "Push-Lifetime: 86400" \
    --data-binary @- \
    "$PUSHGW_URL/metrics/job/harbor-mirror-failure/instance/$APP_NAME-$TENANT_ID" >/dev/null 2>&1; then
  echo "[harbor-mirror-observe] WARN: Pushgateway POST failed (non-fatal)." >&2
fi

echo "[harbor-mirror-observe] Observability emitted for app=$APP_NAME tenant=$TENANT_ID phase=$WORKFLOW_PHASE"
exit 0
`

	return &forge.Script{
		Name:    "harbor-mirror-observe",
		Image:   "alpine/curl:latest",
		Command: []string{"sh"},
		Source:  script,
		// Bound to 30s — observability emission should be fast; if
		// longer, something is wrong (network partition).
		ActiveDeadlineSeconds: forge.Ptr(30),
		Inputs: []forge.Parameter{
			{Name: "app-name"},
			{Name: "tenant-id"},
			{Name: "workflow-phase"},
			{Name: "harbor-endpoint"},
			{Name: "error-msg"},
		},
		Env: []forge.EnvBuilder{
			// Map workflow inputs to env vars consumed by script.
			forge.Env{Name: "APP_NAME", Value: "{{inputs.parameters.app-name}}"},
			forge.Env{Name: "TENANT_ID", Value: "{{inputs.parameters.tenant-id}}"},
			forge.Env{Name: "WORKFLOW_PHASE", Value: "{{inputs.parameters.workflow-phase}}"},
			forge.Env{Name: "HARBOR_ENDPOINT", Value: "{{inputs.parameters.harbor-endpoint}}"},
			forge.Env{Name: "ERROR_MSG", Value: "{{inputs.parameters.error-msg}}"},
			// Loki + Pushgateway URLs — operator overrides via ConfigMap
			// at activation. Defaults assume in-cluster service names.
			forge.Env{Name: "LOKI_PUSH_URL", Value: "http://loki.monitoring.svc.cluster.local:3100/loki/api/v1/push"},
			forge.Env{Name: "PUSHGATEWAY_URL", Value: "http://prometheus-pushgateway.monitoring.svc.cluster.local:9091"},
		},
		Resources: &forge.ResourceRequirements{
			Requests: forge.ResourceList{CPU: "10m", Memory: "32Mi"},
			Limits:   forge.ResourceList{CPU: "100m", Memory: "64Mi"},
		},
		SecurityContext: &model.SecurityContext{
			RunAsNonRoot: forge.Ptr(true),
			RunAsUser:    forge.Ptr(int64(65534)),
			RunAsGroup:   forge.Ptr(int64(65534)),
		},
	}
}
