// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package environmentsadapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/environments"
)

// servePrometheus answers /api/v1/query with body and records the PromQL.
func servePrometheus(t *testing.T, status int, body string) (*PrometheusReplicasReadySource, *string) {
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
	src, err := NewPrometheusReplicasReadySource(srv.URL)
	if err != nil {
		t.Fatalf("NewPrometheusReplicasReadySource: %v", err)
	}
	return src, &query
}

const readinessVector = `{"status":"success","data":{"resultType":"vector","result":[
 {"metric":{"tenant_id":"tn-acme","project":"shop","environment":"production"},"value":[1758800000,"2"]},
 {"metric":{"tenant_id":"tn-acme","project":"blog","environment":"production"},"value":[1758800000,"0"]},
 {"metric":{"tenant_id":"tn-acme"},"value":[1758800000,"5"]}
]}}`

func TestPrometheusReplicasReadySource_ReadsOneSamplePerLabelledEnvironment(t *testing.T) {
	src, query := servePrometheus(t, http.StatusOK, readinessVector)

	samples, err := src.ReplicasReady(context.Background())

	if err != nil {
		t.Fatalf("ReplicasReady: %v", err)
	}
	if *query != ReplicasReadyQuery {
		t.Errorf("query = %q, want ReplicasReadyQuery", *query)
	}
	want := []environments.ReplicasReadySample{
		{Target: environments.EnvironmentTargetKey{TenantID: "tn-acme", ProjectID: "shop", Environment: "production"}, ReplicasReady: 2},
		{Target: environments.EnvironmentTargetKey{TenantID: "tn-acme", ProjectID: "blog", Environment: "production"}, ReplicasReady: 0},
	}
	if len(samples) != len(want) || samples[0] != want[0] || samples[1] != want[1] {
		t.Errorf("samples = %+v, want %+v (the unlabelled workload skipped)", samples, want)
	}
}

// Tenant workloads are Argo Rollouts and kube-state-metrics exports no ReplicaSet
// labels, so the poller reads the ready replicas each cell records per app and
// folds a project's apps into its environment (getloopscale/loop-runtime#107).
func TestReplicasReadyQuery_SumsTheRecordedSeriesPerEnvironment(t *testing.T) {
	const want = `sum by (tenant_id, project, environment) (loopruntime_app:kube_pod_status_ready:sum)`
	if ReplicasReadyQuery != want {
		t.Errorf("ReplicasReadyQuery =\n %s\nwant\n %s", ReplicasReadyQuery, want)
	}
}

func TestPrometheusReplicasReadySource_FailsWhenPrometheusRejectsTheQuery(t *testing.T) {
	src, _ := servePrometheus(t, http.StatusBadRequest, `{"status":"error","errorType":"bad_data","error":"parse error"}`)

	if _, err := src.ReplicasReady(context.Background()); err == nil {
		t.Fatal("ReplicasReady err = nil, want the rejected query reported")
	}
}

func TestPrometheusReplicasReadySource_FailsOnANonVectorResult(t *testing.T) {
	src, _ := servePrometheus(t, http.StatusOK, `{"status":"success","data":{"resultType":"scalar","result":[1758800000,"1"]}}`)

	if _, err := src.ReplicasReady(context.Background()); err == nil {
		t.Fatal("ReplicasReady err = nil, want a non-vector result refused")
	}
}

func TestNewPrometheusReplicasReadySource_RefusesAnUnparseableAddress(t *testing.T) {
	if _, err := NewPrometheusReplicasReadySource("http://[::1"); err == nil {
		t.Fatal("NewPrometheusReplicasReadySource err = nil, want the address refused")
	}
}
