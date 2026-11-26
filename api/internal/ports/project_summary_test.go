// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// T3.2 of plan dashboard-badge-data-plane-poller — closes Red Team
// loop-console#128. RED test guarantee: ProjectSummary MUST expose the
// data-plane-poller snapshot fields (last_replicas_ready_value +
// last_replicas_ready_at) as *POINTER* types tagged `omitempty` so
// projects without a snapshot yet (Cleanroom warm-up / poller disabled)
// emit a wire response WITHOUT those keys — the dashboard maps their
// absence to the honest "Unknown" state per EC-7 no-flicker headroom.
//
// Value-type time.Time with omitempty does NOT omit the zero value
// (`"0001-01-01T00:00:00Z"` is truthy in JS); same trap #118 fixed for
// LastDeployAt. Pointer + omitempty is the only shape that produces an
// absent-key wire when the value is unknown.

package ports

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestProjectSummary_WireFormat_OmitsNilReplicasReadyFields asserts
// that when the two new pointer fields are nil, `json.Marshal` produces
// output that does NOT contain the JSON keys `last_replicas_ready_value`
// or `last_replicas_ready_at`. The dashboard's badge derivation reads
// undefined→Unknown; a serialized `null` OR a zero-value would produce
// a wrong verdict (LiveDown vs Unknown collapse).
func TestProjectSummary_WireFormat_OmitsNilReplicasReadyFields(t *testing.T) {
	s := ProjectSummary{
		ID:                     "proj-abc",
		Name:                   "hello-world",
		TeamID:                 "tn-1234",
		EnvironmentCount:       1,
		ProductionEnvCount:     1,
		LastDeployStatus:       "live",
		AppsCount:              1,
		LatestFramework:        "server",
		LatestBuildEnvironment: "production",
		// LastReplicasReadyValue + LastReplicasReadyAt intentionally
		// nil — the poller has not yet emitted a snapshot for this env.
	}

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal ProjectSummary: %v", err)
	}
	got := string(raw)

	forbiddenKeys := []string{
		"\"last_replicas_ready_value\"",
		"\"last_replicas_ready_at\"",
	}
	for _, key := range forbiddenKeys {
		if strings.Contains(got, key) {
			t.Errorf("wire format leaked %s when nil (Unknown collapses to Down); payload=%s", key, got)
		}
	}
}

// TestProjectSummary_WireFormat_SerializesActiveEnvCount asserts the
// canonical wire shape for the Red Team loop-console#134 fix: the
// `active_env_count` field surfaces the count of env_targets that
// have a currently-serving ACTIVE release. Preview OR permanent env
// kinds both qualify — the truth "LIVE" the dashboard label promises.
//
// Pre-fix bug: dashboard used `production_env_count` (kind='permanent'
// subset only) so a preview env with an ACTIVE release + Live URL
// serving 200 rendered as `LIVE ENVS: 0` — the counter contradicted
// the Live URL shown on the same page.
//
// This test locks the wire contract for the new field so the SQL
// layer + wrapper + dashboard all agree on the semantic.
func TestProjectSummary_WireFormat_SerializesActiveEnvCount(t *testing.T) {
	s := ProjectSummary{
		ID:             "proj-preview-only",
		Name:           "loopzerokit-example",
		TeamID:         "tn-1234",
		ActiveEnvCount: 1, // preview env with ACTIVE release
		// production_env_count intentionally 0 — no permanent env wired
		ProductionEnvCount: 0,
	}

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal ProjectSummary: %v", err)
	}
	got := string(raw)

	if !strings.Contains(got, "\"active_env_count\":1") {
		t.Errorf("wire format missing active_env_count=1 — Red Team #134 fix; payload=%s", got)
	}
	if !strings.Contains(got, "\"production_env_count\":0") {
		t.Errorf("wire format missing production_env_count=0 (must coexist); payload=%s", got)
	}
}

// TestProjectSummary_WireFormat_SerializesNonNilReplicasReadyFields
// asserts the positive case — when the poller has emitted a snapshot,
// both fields serialize under their canonical JSON keys with the raw
// integer value and RFC3339 timestamp. Without this the dashboard
// signal-aware badge never sees the freshness data even when the
// database row is populated.
func TestProjectSummary_WireFormat_SerializesNonNilReplicasReadyFields(t *testing.T) {
	replicas := 3
	when := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	s := ProjectSummary{
		ID:                     "proj-abc",
		Name:                   "hello-world",
		TeamID:                 "tn-1234",
		LastReplicasReadyValue: &replicas,
		LastReplicasReadyAt:    &when,
	}

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal ProjectSummary: %v", err)
	}
	got := string(raw)

	if !strings.Contains(got, "\"last_replicas_ready_value\":3") {
		t.Errorf("wire format missing last_replicas_ready_value=3; payload=%s", got)
	}
	if !strings.Contains(got, "\"last_replicas_ready_at\":\"2026-07-01T12:00:00Z\"") {
		t.Errorf("wire format missing last_replicas_ready_at RFC3339; payload=%s", got)
	}
}
