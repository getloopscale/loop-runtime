// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T3.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure.
// Tests for harbor mirror observability hook + template.

package argo

import (
	"strings"
	"testing"
)

func TestHarborMirrorObserveTemplate_Renders(t *testing.T) {
	tmpl := harborMirrorObserveTemplate()
	if tmpl == nil {
		t.Fatal("expected non-nil template")
	}
	if tmpl.Name != "harbor-mirror-observe" {
		t.Errorf("name = %q, want harbor-mirror-observe", tmpl.Name)
	}
}

// v1.1 EC-NEW-8: curl MUST have --max-time + --connect-timeout to prevent
// hang on Loki/Pushgateway outage. Without timeouts, exit handler hangs
// up to ~120s default → Workflow stuck Cancelling.
func TestHarborMirrorObserveTemplate_CurlHasTimeouts(t *testing.T) {
	tmpl := harborMirrorObserveTemplate()
	if !strings.Contains(tmpl.Source, "--max-time 10") {
		t.Error("script missing --max-time 10 on curl POST — v1.1 EC-NEW-8 regression")
	}
	if !strings.Contains(tmpl.Source, "--connect-timeout 5") {
		t.Error("script missing --connect-timeout 5 on curl POST — v1.1 EC-NEW-8 regression")
	}
}

// v1.1 EC-NEW-7 ADR D5: Pushgateway POST MUST include Push-Lifetime header
// for 24h TTL. Without TTL, stale metrics linger forever.
func TestHarborMirrorObserveTemplate_PushgatewayHasLifetimeHeader(t *testing.T) {
	tmpl := harborMirrorObserveTemplate()
	if !strings.Contains(tmpl.Source, "Push-Lifetime: 86400") {
		t.Error("script missing Push-Lifetime: 86400 (24h TTL) — v1.1 EC-NEW-7 ADR D5 regression")
	}
}

// Loki structured log must include workflow_phase label for filtering
// (EC-15 DOCUMENT — distinguishes transient retry failures from sustained).
func TestHarborMirrorObserveTemplate_LokiLogIncludesWorkflowPhase(t *testing.T) {
	tmpl := harborMirrorObserveTemplate()
	if !strings.Contains(tmpl.Source, "\"workflow_phase\":") {
		t.Error("Loki log missing workflow_phase label — EC-15 filter regression")
	}
}

// Counter labels MUST be bounded cardinality: app + tenant + harbor_endpoint
// (per plan T3.1 Deep Dives, ~100k series max at 20k tenants × 5 apps).
func TestHarborMirrorObserveTemplate_PrometheusCounterLabels(t *testing.T) {
	tmpl := harborMirrorObserveTemplate()
	for _, label := range []string{`app=\"$APP_NAME\"`, `tenant=\"$TENANT_ID\"`, `harbor_endpoint=\"$HARBOR_ENDPOINT\"`} {
		if !strings.Contains(tmpl.Source, label) {
			t.Errorf("counter missing required label: %s", label)
		}
	}
}

// Exit handler MUST exit 0 even when Loki/Pushgateway fail (best-effort
// path — Workflow already continueOn.failed for harbor mirror).
func TestHarborMirrorObserveTemplate_AlwaysExitsZero(t *testing.T) {
	tmpl := harborMirrorObserveTemplate()
	if !strings.Contains(tmpl.Source, "exit 0") {
		t.Error("script must explicit exit 0 (best-effort, observability failure non-fatal)")
	}
}

// LifecycleHook expression must trigger ONLY on Failed status (not
// Succeeded/Cancelled). v1.1 EC-15 DOCUMENT — Cancelled was previously
// causing spurious warns.
func TestHarborMirrorObservabilityHook_TriggersOnlyOnFailed(t *testing.T) {
	hook := harborMirrorObservabilityHook("mirror-harbor-api", "api", "tn-test", "harbor.example.com")
	if !strings.Contains(hook.Expression, `status == "Failed"`) {
		t.Errorf("hook expression should filter on Failed status only, got: %q", hook.Expression)
	}
	if hook.Template != "harbor-mirror-observe" {
		t.Errorf("hook should target harbor-mirror-observe template, got: %q", hook.Template)
	}
}

// Hook passes correct parameters to observe template.
func TestHarborMirrorObservabilityHook_PassesAllParameters(t *testing.T) {
	hook := harborMirrorObservabilityHook("mirror-harbor-api", "api", "tn-abc123", "harbor.example.com")
	if hook.Arguments == nil {
		t.Fatal("hook arguments nil")
	}
	required := map[string]bool{
		"app-name":        false,
		"tenant-id":       false,
		"workflow-phase":  false,
		"harbor-endpoint": false,
		"error-msg":       false,
	}
	for _, p := range hook.Arguments.Parameters {
		required[p.Name] = true
	}
	for name, present := range required {
		if !present {
			t.Errorf("hook missing required parameter: %s", name)
		}
	}
}
