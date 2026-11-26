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

// T3.3 Tempo client hermetic tests using httptest backend.

func TestFetchSpans_ParsesTempoResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify filter param plumbed through
		tags := r.URL.Query().Get("tags")
		if !strings.Contains(tags, "loop-runtime-api") {
			t.Errorf("expected service filter in tags, got %q", tags)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "traces": [
		    {"traceID": "abc", "rootSpan": "GET /deploy", "spans": [{"name": "deploy", "durationNanos": 1000}]},
		    {"traceID": "def", "rootSpan": "GET /deploy", "spans": [{"name": "deploy", "durationNanos": 1500}]}
		  ]
		}`))
	}))
	defer srv.Close()

	q := TempoTraceQuery{
		TempoURL:  srv.URL,
		Service:   "loop-runtime-api",
		Operation: "deploy",
		Window:    1 * time.Minute,
	}
	count, err := FetchSpans(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestAssertSpanCoverageReal_PASS_when_above_threshold(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"traces": [{}, {}, {}, {}, {}]}`))
	}))
	defer srv.Close()

	q := TempoTraceQuery{TempoURL: srv.URL, Service: "loop-runtime-api", Operation: "deploy", Window: 1 * time.Minute}
	count, err := AssertSpanCoverageReal(context.Background(), q, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v (count=%d)", err, count)
	}
	if count != 5 {
		t.Errorf("count = %d, want 5", count)
	}
}

func TestAssertSpanCoverageReal_FAIL_when_below_threshold(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"traces": [{}]}`))
	}))
	defer srv.Close()

	q := TempoTraceQuery{TempoURL: srv.URL, Service: "loop-runtime-api", Operation: "deploy"}
	count, err := AssertSpanCoverageReal(context.Background(), q, 5)
	if err == nil {
		t.Fatal("expected error when count below threshold")
	}
	if !strings.Contains(err.Error(), "below threshold") {
		t.Errorf("error message changed: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (real count returned)", count)
	}
}

func TestAssertSpanCoverageReal_NetworkError_ReturnsMinusOne(t *testing.T) {
	q := TempoTraceQuery{TempoURL: "http://127.0.0.1:1", Service: "loop-runtime-api"}
	count, err := AssertSpanCoverageReal(context.Background(), q, 1)
	if err == nil {
		t.Fatal("expected network error")
	}
	if count != -1 {
		t.Errorf("count = %d, want -1 sentinel for network error", count)
	}
}
