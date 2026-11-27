// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package environmentsadapter

import (
	"context"
	"fmt"
	"math"
	"time"

	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"github.com/getloopscale/loop-runtime/api/internal/environments"
)

// ReplicasReadyQuery sums ready replicas per (tenant, project, environment)
// over the series each cell's Prometheus records per tenant app and the core
// Prometheus federates
// (infra/helm/monitoring/values-tenant-app-recording-rules.yaml).
//
// Tenant workloads run as Argo Rollouts, so no Deployment series describes
// them, and kube-state-metrics exports no ReplicaSet labels to join on. The
// recorded series counts ready pods by the runtime.getloopscale.com labels the manifest
// renderer puts on them, renamed to tenant_id, project, app and environment
// (getloopscale/loop-runtime#107). The sum folds a project's apps, and the cells
// that serve it, into the tuple.
const ReplicasReadyQuery = `sum by (tenant_id, project, environment) (loop_runtime_app:kube_pod_status_ready:sum)`

// PrometheusReplicasReadySource observes data-plane readiness through one
// instant Prometheus query per poll.
type PrometheusReplicasReadySource struct {
	api promv1.API
}

// The port this adapter satisfies, asserted at compile time.
var _ environments.ReplicasReadySource = (*PrometheusReplicasReadySource)(nil)

// NewPrometheusReplicasReadySource returns a source querying the Prometheus
// server at address.
func NewPrometheusReplicasReadySource(address string) (*PrometheusReplicasReadySource, error) {
	client, err := promapi.NewClient(promapi.Config{Address: address})
	if err != nil {
		return nil, fmt.Errorf("prometheus client for %q: %w", address, err)
	}
	return &PrometheusReplicasReadySource{api: promv1.NewAPI(client)}, nil
}

// ReplicasReady implements environments.ReplicasReadySource. A sample without
// all three labels is a workload the platform did not render and is
// skipped, as is a non-finite or negative value, which no replica count is.
func (s *PrometheusReplicasReadySource) ReplicasReady(ctx context.Context) ([]environments.ReplicasReadySample, error) {
	value, _, err := s.api.Query(ctx, ReplicasReadyQuery, time.Now())
	if err != nil {
		return nil, fmt.Errorf("query ready replicas: %w", err)
	}
	vector, ok := value.(model.Vector)
	if !ok {
		return nil, fmt.Errorf("query ready replicas: result is %s, want a vector", value.Type())
	}
	samples := make([]environments.ReplicasReadySample, 0, len(vector))
	for _, sample := range vector {
		target := environments.EnvironmentTargetKey{
			TenantID:    string(sample.Metric["tenant_id"]),
			ProjectID:   string(sample.Metric["project"]),
			Environment: string(sample.Metric["environment"]),
		}
		ready := float64(sample.Value)
		if target.TenantID == "" || target.ProjectID == "" || target.Environment == "" ||
			math.IsNaN(ready) || math.IsInf(ready, 0) || ready < 0 {
			continue
		}
		samples = append(samples, environments.ReplicasReadySample{Target: target, ReplicasReady: int32(min(ready, math.MaxInt32))})
	}
	return samples, nil
}
