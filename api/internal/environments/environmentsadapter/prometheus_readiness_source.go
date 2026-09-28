// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environmentsadapter

import (
	"context"
	"fmt"
	"math"
	"time"

	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"github.com/getloopscale/loopzero/api/internal/environments"
)

// ReplicasReadyQuery sums ready replicas per (tenant, project, environment),
// reading the loopzero.io labels the manifest renderer puts on every workload.
//
// It joins on ReplicaSets, not Deployments: tenant workloads run as Argo
// Rollouts, which own ReplicaSets the way a Deployment does, so
// kube_deployment_labels is empty for them (LIVE-caught 2026-07-04). The sum
// folds a Rollout's older ReplicaSets, idle at zero ready, into the tuple.
// The namespace filter keeps the scan to tenant namespaces.
const ReplicasReadyQuery = `sum by (namespace, label_loopzero_io_tenant, label_loopzero_io_project, label_loopzero_io_environment) (kube_replicaset_status_ready_replicas{namespace=~"loopzero-tn-.*"} * on(namespace, replicaset) group_left(label_loopzero_io_tenant, label_loopzero_io_project, label_loopzero_io_environment) kube_replicaset_labels{namespace=~"loopzero-tn-.*"})`

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
// all three loopzero.io labels is a workload the platform did not render and is
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
			TenantID:    string(sample.Metric["label_loopzero_io_tenant"]),
			ProjectID:   string(sample.Metric["label_loopzero_io_project"]),
			Environment: string(sample.Metric["label_loopzero_io_environment"]),
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
