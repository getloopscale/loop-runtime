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
	"database/sql"
	"log/slog"
	"time"

	"github.com/prometheus/common/model"
)

// PollQuery joins replicas_ready with kube_deployment_labels to
// extract loopzero.io/{tenant,project,environment} from the vector without
// namespace parsing. Canonical Prometheus label-join semantics
// (kube-state-metrics convention). Namespace filter narrows scan to
// loopzero-* only, keeping the query cheap.
const PollQuery = `kube_deployment_status_replicas_ready{namespace=~"loopzero-.*"} * on(namespace, deployment) group_left(label_loopzero_io_tenant, label_loopzero_io_project, label_loopzero_io_environment) kube_deployment_labels{namespace=~"loopzero-.*"}`

// PrometheusInstantClient abstracts Prometheus instant query for testing (DIP).
// Production uses github.com/prometheus/client_golang/api/prometheus/v1;
// tests inject a fake returning a fixed model.Vector.
type PrometheusInstantClient interface {
	Query(ctx context.Context, promQL string, t time.Time) (model.Vector, error)
}

// DataPlanePoller is a controller-runtime manager.Runnable that scrapes
// data-plane readiness signals from Prometheus and persists snapshots
// to env_targets.
type DataPlanePoller struct {
	prom     PrometheusInstantClient
	db       *sql.DB
	interval time.Duration
	logger   *slog.Logger
}

// NewDataPlanePoller constructs a poller with the given dependencies.
// interval controls the scrape cadence; 30 s is the canonical default.
func NewDataPlanePoller(prom PrometheusInstantClient, db *sql.DB, interval time.Duration, logger *slog.Logger) *DataPlanePoller {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &DataPlanePoller{prom: prom, db: db, interval: interval, logger: logger}
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
				p.logger.Warn("data-plane-poller: tick failed, preserving last-known-good", "err", err)
			}
		}
	}
}

// Tick executes one scrape-and-persist cycle. Exposed so tests can
// exercise a single iteration deterministically without goroutines.
func (p *DataPlanePoller) Tick(ctx context.Context) error {
	vec, err := p.prom.Query(ctx, PollQuery, time.Now())
	if err != nil {
		return err
	}
	now := time.Now()
	for _, sample := range vec {
		tenant := string(sample.Metric["label_loopzero_io_tenant"])
		project := string(sample.Metric["label_loopzero_io_project"])
		env := string(sample.Metric["label_loopzero_io_environment"])
		if tenant == "" || project == "" || env == "" {
			// Deployment missing canonical labels — skip. Legitimate
			// non-LoopZero Deployments in loopzero-* namespaces (unlikely but
			// defensive) don't have these labels and shouldn't corrupt
			// any snapshot row.
			continue
		}
		value := int64(sample.Value)
		if _, err := p.db.ExecContext(ctx, `
			UPDATE environment_targets
			SET last_replicas_ready_value = $1, last_replicas_ready_at = $2
			WHERE tenant_id = $3 AND project_id = $4 AND environment = $5
		`, value, now, tenant, project, env); err != nil {
			p.logger.Warn("data-plane-poller: update failed", "tenant", tenant, "project", project, "env", env, "err", err)
		}
	}
	return nil
}
