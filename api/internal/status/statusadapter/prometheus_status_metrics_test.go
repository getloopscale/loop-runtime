// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statusadapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// servePrometheus answers /api/v1/query with body and records the PromQL.
func servePrometheus(t *testing.T, status int, body string) (*PrometheusStatusMetrics, *string) {
	t.Helper()
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("path = %q, want /api/v1/query", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		query = r.Form.Get("query")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	src, err := NewPrometheusStatusMetrics(srv.URL)
	if err != nil {
		t.Fatalf("NewPrometheusStatusMetrics: %v", err)
	}
	return src, &query
}

func vectorOf(value string) string {
	return `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1758800000,"` + value + `"]}]}}`
}

const emptyVector = `{"status":"success","data":{"resultType":"vector","result":[]}}`

const stagingMatcher = `tenant_id="tn-acme",project="shop",environment="staging"`

func TestPrometheusStatusMetrics_ReadyReplicas_SumsTheRecordedSeriesOfTheEnvironment(t *testing.T) {
	src, query := servePrometheus(t, http.StatusOK, vectorOf("2"))

	ready, err := src.ReadyReplicas(context.Background(), "tn-acme", "shop", "staging", nil)

	if err != nil || ready == nil || *ready != 2 {
		t.Fatalf("ready = %v, err = %v, want 2", ready, err)
	}
	if want := `sum(loopruntime_app:kube_pod_status_ready:sum{` + stagingMatcher + `})`; *query != want {
		t.Errorf("query = %s\n want = %s", *query, want)
	}
}

func TestPrometheusStatusMetrics_ReadyReplicas_ZeroIsAMeasuredValue(t *testing.T) {
	src, _ := servePrometheus(t, http.StatusOK, vectorOf("0"))

	ready, err := src.ReadyReplicas(context.Background(), "tn-acme", "shop", "staging", nil)

	if err != nil || ready == nil || *ready != 0 {
		t.Fatalf("ready = %v, err = %v, want a measured 0", ready, err)
	}
}

func TestPrometheusStatusMetrics_ReadyReplicas_NoSeriesIsNoValue(t *testing.T) {
	src, _ := servePrometheus(t, http.StatusOK, emptyVector)

	ready, err := src.ReadyReplicas(context.Background(), "tn-acme", "shop", "staging", nil)

	if err != nil || ready != nil {
		t.Fatalf("ready = %v, err = %v, want no value and no error", ready, err)
	}
}

func TestPrometheusStatusMetrics_ReadyReplicas_ReportsAPrometheusFailure(t *testing.T) {
	src, _ := servePrometheus(t, http.StatusServiceUnavailable, `{"status":"error","error":"down"}`)

	if _, err := src.ReadyReplicas(context.Background(), "tn-acme", "shop", "staging", nil); err == nil {
		t.Fatal("a failed query must be reported, not read as no replicas")
	}
}

func TestPrometheusStatusMetrics_ServerErrorPercent_IsTheShareOf5xxOverTheWindow(t *testing.T) {
	src, query := servePrometheus(t, http.StatusOK, vectorOf("0.25"))

	pct, err := src.ServerErrorPercent(context.Background(), "tn-acme", "shop", "staging", time.Hour)

	if err != nil || pct == nil || *pct != 0.25 {
		t.Fatalf("pct = %v, err = %v, want 0.25", pct, err)
	}
	for _, want := range []string{
		`avg_over_time(loopruntime_app_status_class:traefik_service_requests:rate5m{status_class="5xx",` + stagingMatcher + `}[1h])`,
		`avg_over_time(loopruntime_app:traefik_service_requests:rate5m{` + stagingMatcher + `}[1h])`,
		`or vector(0)`,
		`* 100`,
	} {
		if !strings.Contains(*query, want) {
			t.Errorf("query lacks %s\n query = %s", want, *query)
		}
	}
}

func TestPrometheusStatusMetrics_ServerErrorPercent_NoTrafficIsNoValue(t *testing.T) {
	for name, body := range map[string]string{
		"no request series":       emptyVector,
		"zero requests, 0 over 0": vectorOf("NaN"),
	} {
		t.Run(name, func(t *testing.T) {
			src, _ := servePrometheus(t, http.StatusOK, body)

			pct, err := src.ServerErrorPercent(context.Background(), "tn-acme", "shop", "staging", time.Hour)

			if err != nil || pct != nil {
				t.Fatalf("pct = %v, err = %v, want no value and no error", pct, err)
			}
		})
	}
}

// An app that left the environment keeps its last sample in the federated
// series for minutes. Counting only the apps of the active release leaves it out.
func TestPrometheusStatusMetrics_ReadyReplicas_CountsOnlyTheNamedApps(t *testing.T) {
	src, query := servePrometheus(t, http.StatusOK, vectorOf("1"))

	ready, err := src.ReadyReplicas(context.Background(), "tn-acme", "shop", "staging", []string{"app", "worker.v2"})

	if err != nil || ready == nil || *ready != 1 {
		t.Fatalf("ready = %v, err = %v, want 1", ready, err)
	}
	if want := `sum(loopruntime_app:kube_pod_status_ready:sum{` + stagingMatcher + `,app=~"app|worker\\.v2"})`; *query != want {
		t.Errorf("query = %s\n want = %s", *query, want)
	}
}
