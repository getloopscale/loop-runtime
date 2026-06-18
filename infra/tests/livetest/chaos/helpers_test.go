// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package chaos

import (
	"context"
	"strings"
	"testing"
	"time"
)

// T3.3 helper-level hermetic tests. Validates type contracts + AssertSpanCoverage
// stub behavior. LIVE chaos injection lives in chaos_live_test.go gated por
// build tag `live`.

func TestManifest_TypeFields(t *testing.T) {
	m := Manifest{
		ID:         "C01",
		Path:       "infra/tests/chaos/manifests/pod-kill-api-leader.yaml",
		Kind:       "PodChaos",
		Duration:   60 * time.Second,
		Hypothesis: "Leader election survives kill; no pod restart loop",
	}
	if m.ID != "C01" {
		t.Errorf("ID = %q", m.ID)
	}
	if m.Kind != "PodChaos" {
		t.Errorf("Kind = %q", m.Kind)
	}
	if m.Duration != 60*time.Second {
		t.Errorf("Duration = %v", m.Duration)
	}
}

func TestChaosResult_VerdictValues(t *testing.T) {
	// Verify ChaosResult struct shape matches v1 contra-prova verdict bag
	cases := []string{"PASS", "FAIL", "INCONCLUSIVE"}
	for _, v := range cases {
		r := ChaosResult{Verdict: v}
		if r.Verdict != v {
			t.Errorf("Verdict roundtrip failed for %q", v)
		}
	}
}

func TestAssertSpanCoverage_StubReturnsNotYetWired(t *testing.T) {
	count, err := AssertSpanCoverage(context.Background(), TempoTraceQuery{
		Service:   "loopzero-api",
		Operation: "deploy",
		Window:    1 * time.Minute,
	}, 5)
	if err == nil {
		t.Fatal("expected stub error from AssertSpanCoverage")
	}
	if !strings.Contains(err.Error(), "Tempo client not yet wired") {
		t.Errorf("error message changed: %v", err)
	}
	if count != -1 {
		t.Errorf("count = %d, want -1 (sentinel)", count)
	}
}
