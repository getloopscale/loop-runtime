#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# T0.2 Cenário B POC hermetic render tests.
# Validates that the 3 new charts (argo-ci-namespace, argo-events,
# loopzero-ci-workflows) render valid Kubernetes manifests + the gate
# list in loopzero-ci-workflows/values.yaml parity-matches the taskfile.
#
# Per plan T7.1 of docs/plans/2026-05-27-t0.2-cenario-b-argo-workflows-ci-poc-plan.md.
#
# Exit codes:
#   0 = all PASS
#   1 = any assertion failed

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

PASS=0
FAIL=0

log() {
  echo "[t0.2-render-test] $*"
}

assert() {
  local desc="$1"
  local cmd="$2"
  if eval "$cmd" >/dev/null 2>&1; then
    log "PASS: $desc"
    PASS=$((PASS + 1))
  else
    log "FAIL: $desc"
    log "      Command: $cmd"
    FAIL=$((FAIL + 1))
  fi
}

# ---------------------------------------------------------------------------
# Test 1: All 3 charts lint clean
# ---------------------------------------------------------------------------

log "=== Helm lint ==="
for chart in infra/helm/argo-ci-namespace infra/helm/argo-events infra/helm/loopzero-ci-workflows; do
  assert "helm lint $chart" "helm lint $chart"
done

# ---------------------------------------------------------------------------
# Test 2: All 3 charts render valid YAML
# ---------------------------------------------------------------------------

log "=== Helm template renders ==="
for chart in infra/helm/argo-ci-namespace infra/helm/argo-events infra/helm/loopzero-ci-workflows; do
  assert "helm template $chart" "helm template $chart >/dev/null"
done

# ---------------------------------------------------------------------------
# Test 3: argo-ci-namespace produces expected resources
# ---------------------------------------------------------------------------

log "=== argo-ci-namespace resource count ==="
RESOURCES=$(helm template infra/helm/argo-ci-namespace 2>/dev/null | grep -E "^kind:" | awk '{print $2}' | sort | uniq)
# Namespace creation DELEGATED to Helmfile createNamespace:true (2026-05-27 fix)
# per FAANG single-writer principle. Chart only manages content (RBAC + SA + NP).
assert "rbac.yaml renders Role" "echo \"$RESOURCES\" | grep -q '^Role$'"
assert "rbac.yaml renders RoleBinding (NOT ClusterRoleBinding per ADR D3)" "echo \"$RESOURCES\" | grep -q RoleBinding"
assert "serviceaccount.yaml renders ServiceAccount" "echo \"$RESOURCES\" | grep -q ServiceAccount"
assert "networkpolicy.yaml renders NetworkPolicy" "echo \"$RESOURCES\" | grep -q NetworkPolicy"
assert "chart does NOT manage Namespace (Helmfile owns ns lifecycle)" "! echo \"$RESOURCES\" | grep -q '^Namespace$'"

# Confirm NO ClusterRoleBinding (boundary mitigation per ADR D3)
CRB_COUNT=$(helm template infra/helm/argo-ci-namespace 2>/dev/null | grep -c "^kind: ClusterRoleBinding" || true)
assert "ZERO ClusterRoleBinding (D3 namespace-scoped only)" "[ \"$CRB_COUNT\" -eq 0 ]"

# ---------------------------------------------------------------------------
# Test 4: loopzero-ci-workflows gate list parity with taskfile
# ---------------------------------------------------------------------------

log "=== Gate list parity (values.yaml ↔ taskfiles/quality.yml) ==="

# Extract gate names from values.yaml
VALUES_GATES=$(grep -E "^  - gateName:" infra/helm/loopzero-ci-workflows/values.yaml | awk '{print $3}' | sort | uniq)

# Extract gate names from taskfile — ONLY actual command lines
# (start with `- /tmp/loopzero-ops quality`), NOT descriptions/comments.
TASKFILE_GATES=$(grep -E "^\s+- /tmp/loopzero-ops quality " taskfiles/quality.yml | \
  sed -E 's/.*loopzero-ops quality ([a-z-]+).*/\1/' | \
  grep -vE "(^\s*$|--check)" | sort | uniq)

# Diff
MISSING_FROM_VALUES=$(comm -23 <(echo "$TASKFILE_GATES") <(echo "$VALUES_GATES") | head -10)
EXTRA_IN_VALUES=$(comm -13 <(echo "$TASKFILE_GATES") <(echo "$VALUES_GATES") | head -10)

if [ -n "$MISSING_FROM_VALUES" ]; then
  log "FAIL: gates in taskfile but missing from values.yaml:"
  echo "$MISSING_FROM_VALUES" | sed 's/^/  - /'
  FAIL=$((FAIL + 1))
else
  log "PASS: all taskfile gates present in values.yaml"
  PASS=$((PASS + 1))
fi

if [ -n "$EXTRA_IN_VALUES" ]; then
  log "FAIL: gates in values.yaml but missing from taskfile (orphan):"
  echo "$EXTRA_IN_VALUES" | sed 's/^/  - /'
  FAIL=$((FAIL + 1))
else
  log "PASS: no orphan gates in values.yaml"
  PASS=$((PASS + 1))
fi

# ---------------------------------------------------------------------------
# Test 5: WorkflowTemplate has critical safety properties
# ---------------------------------------------------------------------------

log "=== quality-gate WorkflowTemplate safety ==="
WT_YAML=$(helm template infra/helm/loopzero-ci-workflows 2>/dev/null | awk '/^---$/{p=0} /kind: WorkflowTemplate/{p=1} p')

assert "WorkflowTemplate has activeDeadlineSeconds" "echo \"$WT_YAML\" | grep -q 'activeDeadlineSeconds: 900'"
assert "WorkflowTemplate has onExit handler (PR status posting)" "echo \"$WT_YAML\" | grep -q 'onExit: post-status'"
assert "WorkflowTemplate uses loopzero-ci-runner SA" "echo \"$WT_YAML\" | grep -q 'serviceAccountName: loopzero-ci-runner'"

# ---------------------------------------------------------------------------
# Test 6: EventSource has HMAC + filter
# ---------------------------------------------------------------------------

log "=== EventSource validation ==="
ES_YAML=$(helm template infra/helm/loopzero-ci-workflows 2>/dev/null | awk '/^---$/{p=0} /kind: EventSource/{p=1} p')

assert "EventSource has authSecret (HMAC validation)" "echo \"$ES_YAML\" | grep -q 'authSecret:'"
assert "EventSource has filter expression (PR events only)" "echo \"$ES_YAML\" | grep -q 'pull_request_opened'"

# ---------------------------------------------------------------------------
# Test 7: PR status context name = "quality" (ADR D4 fixed)
# ---------------------------------------------------------------------------

log "=== PR status context name (ADR D4) ==="
# Confirm "quality" is the configured prStatusContext value in rendered output.
# The literal string appears in the WorkflowTemplate env var
# `STATUS_CONTEXT: "quality"` (D4 fixed name).
assert "Rendered prStatusContext = quality (D4 fixed)" "helm template infra/helm/loopzero-ci-workflows 2>/dev/null | grep -F 'STATUS_CONTEXT' -A 1 | grep -qF '\"quality\"'"

# ---------------------------------------------------------------------------
# Test 8: 4 CronWorkflows present with correct schedules
# ---------------------------------------------------------------------------

log "=== CronWorkflows ==="
CW_NAMES=$(helm template infra/helm/loopzero-ci-workflows 2>/dev/null | grep -E "^kind: CronWorkflow$" -A 5 | grep "^  name:" | awk '{print $2}' | sort | uniq)

for expected in drill-watchdog weekly-soak coverage-report cosign-deadline; do
  assert "CronWorkflow $expected present" "echo \"$CW_NAMES\" | grep -q '^$expected$'"
done

# Verify schedules (use grep -F fixed-string to avoid regex shell-escaping)
# Argo Workflows v4.x: schedules is plural array — schedule strings appear under `- "..."`
assert "drill-watchdog schedule every 6h" "helm template infra/helm/loopzero-ci-workflows 2>/dev/null | grep -qF '\"0 */6 * * *\"'"
assert "weekly-soak schedule Sunday 02:00" "helm template infra/helm/loopzero-ci-workflows 2>/dev/null | grep -qF '\"0 2 * * 0\"'"
assert "coverage-report schedule Monday 09:00" "helm template infra/helm/loopzero-ci-workflows 2>/dev/null | grep -qF '\"0 9 * * 1\"'"
assert "cosign-deadline schedule daily 00:00" "helm template infra/helm/loopzero-ci-workflows 2>/dev/null | grep -qF '\"0 0 * * *\"'"
assert "v4.x schedules plural array (not v3 singular)" "helm template infra/helm/loopzero-ci-workflows 2>/dev/null | grep -qE 'schedules:'"

# ---------------------------------------------------------------------------
# Test 9: Cenário A POC files PRESERVED (ADR D5)
# ---------------------------------------------------------------------------

log "=== ADR D5: Cenário A POC files preserved ==="
assert "values-actions-poc.yaml.disabled preserved" "[ -f infra/helm/gitea/values-actions-poc.yaml.disabled ]"
assert ".gitea/workflows/quality.yml.example preserved" "[ -f .gitea/workflows/quality.yml.example ]"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

log ""
log "=== Summary ==="
log "PASS: $PASS"
log "FAIL: $FAIL"

if [ "$FAIL" -gt 0 ]; then
  log "RESULT: FAIL"
  exit 1
fi

log "RESULT: PASS"
exit 0
