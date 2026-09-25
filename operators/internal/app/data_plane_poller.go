// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// T2.1 of plan dashboard-badge-data-plane-poller — closes Red Team
// loopzero-cloud#128 (badge lies "Live" when data plane serves 502).
//
// Periodically scrapes kube_deployment_status_replicas_ready from
// Prometheus and persists per-(tenant, project, environment) snapshots
// into env_targets. Consumed by ListProjectsWithSummaryByTenant LATERAL
// JOIN (T3.1) so the dashboard /projects badge can distinguish
// control-plane ACTIVE + data-plane-serving from control-plane ACTIVE
// + data-plane-down.
//
// Design:
//   - Runs as a controller-runtime manager.Runnable in loopzero-app-operator.
//   - Leader-elected (NeedLeaderElection = true) so exactly one instance
//     scrapes when multiple operator replicas run.
//   - Batched fan-out: ONE instant Prometheus query per tick with
//     namespace regex + label-join reading loopzero.io/{tenant,project,
//     environment} labels emitted by the manifest renderer
//     (renderer_helpers.go:61-64). Zero namespace parsing — labels come
//     from Prometheus directly (EC-5 anti-drift, plan blueprint Q9).
//   - Preserve-last-known-good on scrape failure (ADR-3): PG rows are
//     untouched when Prometheus is unreachable; the 5-min staleness
//     threshold in the dashboard degrades badges to "Unknown" without
//     the poller having to overwrite with NULL.
//
// Refs: Red Team loopzero-cloud#128, plan dashboard-badge-data-plane-poller.

package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// PromV1Adapter wraps prometheus/client_golang/api/prometheus/v1.API to
// satisfy PrometheusInstantClient. The upstream `Query` returns
// (model.Value, Warnings, error); this adapter type-asserts the Value
// as model.Vector (canonical for instant queries) and surfaces a clear
// error on shape mismatch so misconfigured queries fail loud.
type PromV1Adapter struct{ API promv1.API }

// Query implements PrometheusInstantClient.
func (a *PromV1Adapter) Query(ctx context.Context, q string, t time.Time) (model.Vector, error) {
	val, _, err := a.API.Query(ctx, q, t)
	if err != nil {
		return nil, err
	}
	vec, ok := val.(model.Vector)
	if !ok {
		return nil, fmt.Errorf("prometheus query returned %T, expected model.Vector", val)
	}
	return vec, nil
}

// PollQuery joins replicas_ready with kube_replicaset_labels to extract
// loopzero.io/{tenant,project,environment} from the vector without namespace
// parsing.
//
// LIVE-caught 2026-07-04: tenant workloads run as Argo Rollouts (not raw
// K8s Deployments), so `kube_deployment_labels{namespace=~"loopzero-tn-.*"}`
// returns an empty vector even when kube-state-metrics has the
// deployments allowlist enabled. Argo Rollouts DOES create backing
// ReplicaSets (Rollout owns RS the same way Deployment does), and those
// RSes DO carry the canonical loopzero.io labels — so join on ReplicaSet
// instead. The `max(...) by (namespace, label_...)` collapses inactive
// RSes (rollout keeps prior generations at 0 ready) so the sum of
// replicas_ready lands under the correct (tenant,project,env) tuple.
// Namespace filter narrows the scan to loopzero-tn-* (production/preview
// tenant namespaces) to skip mgmt-side deployments.
const PollQuery = `sum by (namespace, label_loopzero_io_tenant, label_loopzero_io_project, label_loopzero_io_environment) (kube_replicaset_status_ready_replicas{namespace=~"loopzero-tn-.*"} * on(namespace, replicaset) group_left(label_loopzero_io_tenant, label_loopzero_io_project, label_loopzero_io_environment) kube_replicaset_labels{namespace=~"loopzero-tn-.*"})`

// PrometheusInstantClient abstracts Prometheus instant query for testing (DIP).
// Production uses github.com/prometheus/client_golang/api/prometheus/v1;
// tests inject a fake returning a fixed model.Vector.
type PrometheusInstantClient interface {
	Query(ctx context.Context, promQL string, t time.Time) (model.Vector, error)
}

// EnvironmentTarget identifies the (tenant, project, environment) a readiness
// sample belongs to, read from the loopzero.io labels on the ReplicaSet.
type EnvironmentTarget struct {
	Tenant      string
	Project     string
	Environment string
}

func (t EnvironmentTarget) String() string {
	return t.Tenant + "/" + t.Project + "/" + t.Environment
}

// ErrEnvironmentTargetNotFound is returned by a ReadinessSnapshotSink when no
// stored environment target matches the sample, so nothing was recorded.
var ErrEnvironmentTargetNotFound = errors.New("no environment target matches the sample")

// ReadinessSnapshotSink is the port through which the poller records a
// data-plane readiness snapshot. The poller depends on this contract, not on
// where the snapshot is stored; PostgresReadinessSink is the one adapter.
type ReadinessSnapshotSink interface {
	RecordReplicasReady(ctx context.Context, target EnvironmentTarget, replicasReady int64, at time.Time) error
}

// DataPlanePoller is a controller-runtime manager.Runnable that scrapes
// data-plane readiness signals from Prometheus and records one snapshot per
// environment target through a ReadinessSnapshotSink.
type DataPlanePoller struct {
	prom     PrometheusInstantClient
	sink     ReadinessSnapshotSink
	interval time.Duration
	logger   *slog.Logger
}

// NewDataPlanePoller constructs a poller with the given dependencies.
// interval controls the scrape cadence; 30 s is the canonical default.
func NewDataPlanePoller(prom PrometheusInstantClient, sink ReadinessSnapshotSink, interval time.Duration, logger *slog.Logger) *DataPlanePoller {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &DataPlanePoller{prom: prom, sink: sink, interval: interval, logger: logger}
}

// NeedLeaderElection implements manager.LeaderElectionRunnable. Returning
// true ensures only the leader operator replica scrapes (EC-6 — prevents
// double-scrape when two operator pods run).
func (p *DataPlanePoller) NeedLeaderElection() bool { return true }

// Start implements manager.Runnable. Runs until ctx is canceled.
func (p *DataPlanePoller) Start(ctx context.Context) error {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := p.Tick(ctx); err != nil {
				p.logger.Warn("data-plane-poller: tick failed; targets not written keep their last snapshot", "err", err)
			}
		}
	}
}

// Tick executes one scrape-and-persist cycle. Exposed so tests can
// exercise a single iteration deterministically without goroutines.
//
// Every sample is attempted; the returned error joins the write failures,
// each naming its target. A sample whose target has no stored row is not a
// write failure — it is logged, so a snapshot that stops moving is visible.
func (p *DataPlanePoller) Tick(ctx context.Context) error {
	vec, err := p.prom.Query(ctx, PollQuery, time.Now())
	if err != nil {
		return err
	}
	now := time.Now()
	var failures []error
	for _, sample := range vec {
		target := EnvironmentTarget{
			Tenant:      string(sample.Metric["label_loopzero_io_tenant"]),
			Project:     string(sample.Metric["label_loopzero_io_project"]),
			Environment: string(sample.Metric["label_loopzero_io_environment"]),
		}
		if target.Tenant == "" || target.Project == "" || target.Environment == "" {
			// ReplicaSet missing canonical labels — skip. Non-LoopZero
			// workloads in loopzero-* namespaces must not corrupt a snapshot.
			continue
		}
		err := p.sink.RecordReplicasReady(ctx, target, int64(sample.Value), now)
		switch {
		case err == nil:
		case errors.Is(err, ErrEnvironmentTargetNotFound):
			p.logger.Warn("data-plane-poller: no environment target for sample; its snapshot is not recorded",
				"target", target.String())
		default:
			failures = append(failures, fmt.Errorf("record readiness for %s: %w", target, err))
		}
	}
	return errors.Join(failures...)
}
