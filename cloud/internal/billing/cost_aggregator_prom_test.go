// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billing

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestHTTPPromQuerier_QueryCounterByTenant_ParsesVector verifies the happy
// path: Prometheus returns a Vector result with two samples (different
// tenants) and the querier extracts {tenant → value}.
func TestHTTPPromQuerier_QueryCounterByTenant_ParsesVector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/api/v1/query") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "vector",
				"result": [
					{
						"metric": {"tenant": "tn-1"},
						"value": [1715520000, "5368709120"]
					},
					{
						"metric": {"tenant": "tn-2"},
						"value": [1715520000, "1073741824"]
					}
				]
			}
		}`))
	}))
	defer srv.Close()

	q, err := NewHTTPPromQuerier(srv.URL, slog.Default())
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}

	values, err := q.QueryCounterByTenant(context.Background(),
		`sum by (tenant) (loopzero_egress_bytes_total)`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if len(values) != 2 {
		t.Errorf("want 2 tenants, got %d: %v", len(values), values)
	}
	if values["tn-1"] != 5368709120 {
		t.Errorf("tn-1 = %v, want 5368709120", values["tn-1"])
	}
	if values["tn-2"] != 1073741824 {
		t.Errorf("tn-2 = %v, want 1073741824", values["tn-2"])
	}
}

// TestHTTPPromQuerier_HTTPError_SurfacesAsError verifies that HTTP-level
// failures (5xx, timeout, refused) bubble up so CostAggregator can log
// and skip the metric tick (fail-open posture).
func TestHTTPPromQuerier_HTTPError_SurfacesAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"status":"error","errorType":"internal","error":"boom"}`))
	}))
	defer srv.Close()

	q, _ := NewHTTPPromQuerier(srv.URL, slog.Default())
	_, err := q.QueryCounterByTenant(context.Background(), `up`)
	if err == nil {
		t.Error("want error on 500, got nil")
	}
}

// TestHTTPPromQuerier_MissingTenantLabel_Skipped verifies that samples
// without a `tenant` label are silently dropped (no panic, no spurious
// empty-key entries).
func TestHTTPPromQuerier_MissingTenantLabel_Skipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "vector",
				"result": [
					{"metric": {"job": "loopzero-api"}, "value": [1, "42"]},
					{"metric": {"tenant": "tn-real"}, "value": [1, "100"]}
				]
			}
		}`))
	}))
	defer srv.Close()

	q, _ := NewHTTPPromQuerier(srv.URL, slog.Default())
	values, err := q.QueryCounterByTenant(context.Background(), `loopzero_x`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(values) != 1 {
		t.Errorf("want 1 tenant (no-tenant-label sample skipped), got %d: %v",
			len(values), values)
	}
	if _, has := values[""]; has {
		t.Error("empty-key entry leaked from missing tenant label")
	}
}

// TestHTTPPromQuerier_EmptyURL_ReturnsError validates constructor input.
func TestHTTPPromQuerier_EmptyURL_ReturnsError(t *testing.T) {
	if _, err := NewHTTPPromQuerier("", slog.Default()); err == nil {
		t.Error("want error for empty URL")
	}
}

// TestHTTPPromQuerier_WithQueryTimeout_Honored verifies that the
// per-query timeout fires when Prometheus is slow.
func TestHTTPPromQuerier_WithQueryTimeout_Honored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer srv.Close()

	q, _ := NewHTTPPromQuerier(srv.URL, slog.Default())
	q.WithQueryTimeout(50 * time.Millisecond)

	start := time.Now()
	_, err := q.QueryCounterByTenant(context.Background(), `up`)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("want timeout error")
	}
	if elapsed > 150*time.Millisecond {
		t.Errorf("timeout not enforced — query took %v", elapsed)
	}
}

// TestHTTPPromQuerier_ScalarResult_ReturnsEmptyMap verifies that
// unexpected result types (e.g., a query like `count(up)` returns a
// Scalar) yield empty map without error, so the aggregator simply
// skips that metric this tick.
func TestHTTPPromQuerier_ScalarResult_ReturnsEmptyMap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {"resultType": "scalar", "result": [1715520000, "42"]}
		}`))
	}))
	defer srv.Close()

	q, _ := NewHTTPPromQuerier(srv.URL, slog.Default())
	values, err := q.QueryCounterByTenant(context.Background(), `count(up)`)
	if err != nil {
		t.Errorf("scalar result must not error: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("scalar → want empty map, got %v", values)
	}
}
