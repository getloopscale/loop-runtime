// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package chaos

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// T3.3 helper-level hermetic tests. Validates type contracts + AssertSpanCoverage
// delegation behavior. LIVE chaos injection lives in chaos_live_test.go gated por
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
	// Verify ChaosResult struct shape matches the v1 counter-proof verdict bag
	cases := []string{"PASS", "FAIL", "INCONCLUSIVE"}
	for _, v := range cases {
		r := ChaosResult{Verdict: v}
		if r.Verdict != v {
			t.Errorf("Verdict roundtrip failed for %q", v)
		}
	}
}

// M10 T4.1: AssertSpanCoverage is no longer a stub — it delegates to the real
// Tempo HTTP client. Hermetic proof via an httptest Tempo backend (no LIVE
// cluster). The LIVE query fires only when a real TempoURL (DefaultTempoURL) is
// set; here the query points at the httptest server.
func TestAssertSpanCoverage_DelegatesToRealClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"traces":[
		  {"traceID":"a","rootSpan":"deploy","spans":[{"name":"deploy","durationNanos":1}]},
		  {"traceID":"b","rootSpan":"deploy","spans":[{"name":"deploy","durationNanos":2}]}
		]}`))
	}))
	defer srv.Close()

	q := TempoTraceQuery{TempoURL: srv.URL, Service: "loop-runtime-api", Operation: "deploy", Window: time.Minute}

	// Above threshold -> real trace count, no error (proves delegation).
	count, err := AssertSpanCoverage(context.Background(), q, 2)
	if err != nil {
		t.Fatalf("delegated call errored: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2 (delegated real count)", count)
	}

	// Below threshold -> real threshold error, NOT the retired 'not yet wired' stub.
	_, err = AssertSpanCoverage(context.Background(), q, 5)
	if err == nil {
		t.Fatal("expected below-threshold error from the delegated real client")
	}
	if strings.Contains(err.Error(), "not yet wired") {
		t.Errorf("still returning the stub error — de-skeleton incomplete: %v", err)
	}
}
