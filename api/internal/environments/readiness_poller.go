// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environments

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
)

// EnvironmentTargetKey identifies one (tenant, project, environment) — the
// key of an environment_targets row.
type EnvironmentTargetKey struct {
	TenantID    string
	ProjectID   string
	Environment string
}

func (k EnvironmentTargetKey) String() string {
	return k.TenantID + "/" + k.ProjectID + "/" + k.Environment
}

// ReplicasReadySample is how many replicas of one environment's workloads
// were ready when the data plane was observed.
type ReplicasReadySample struct {
	Target        EnvironmentTargetKey
	ReplicasReady int32
}

// ReplicasReadySource observes the data plane. environmentsadapter's
// Prometheus source is the production implementation.
type ReplicasReadySource interface {
	ReplicasReady(ctx context.Context) ([]ReplicasReadySample, error)
}

// ReadinessSnapshotStore persists one readiness snapshot. found is false
// when no environment target matches. db/store.PostgresEnvironmentTargetStore
// implements it.
type ReadinessSnapshotStore interface {
	RecordReplicasReady(ctx context.Context, tenantID, projectID, env string, replicasReady int32, at time.Time) (found bool, err error)
}

// ReadinessPoller records, per environment target, how many replicas the data
// plane is serving, so the dashboard's project badge can tell a deploy that
// is ACTIVE and serving from one that is ACTIVE and down (loopzero-cloud#128).
//
// It lives in the api because the api owns environment_targets: the
// app-operator used to run it and write the table through raw SQL over a
// DSN of its own, coupling a workload controller to another component's
// schema. It runs as a background loop, so only the elected leader polls.
//
// A failed scrape writes nothing: every snapshot keeps its last value, and the
// dashboard's staleness threshold degrades the badge to "Unknown" on its own.
type ReadinessPoller struct {
	source   ReplicasReadySource
	store    ReadinessSnapshotStore
	interval time.Duration
	now      func() time.Time
	logger   *slog.Logger
}

// NewReadinessPoller returns a poller observing source every interval.
func NewReadinessPoller(source ReplicasReadySource, store ReadinessSnapshotStore, interval time.Duration, logger *slog.Logger) *ReadinessPoller {
	return &ReadinessPoller{source: source, store: store, interval: interval, now: time.Now, logger: logger}
}

// Start polls until ctx is cancelled.
func (p *ReadinessPoller) Start(ctx context.Context) {
	p.logger.Info("data-plane readiness poller started", "interval", p.interval)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			p.logger.Info("data-plane readiness poller stopped")
			return
		case <-ticker.C:
			if err := p.Tick(ctx); err != nil {
				p.logger.Warn("data-plane readiness poller: tick failed; targets not written keep their last snapshot", "error", err)
			}
		}
	}
}

// Tick runs one scrape-and-record cycle. Every sample is attempted; the
// returned error joins the write failures, each naming its target. A sample
// with no target row is logged and counted rather than failed: it is a
// workload the platform no longer tracks, not a write that went wrong.
func (p *ReadinessPoller) Tick(ctx context.Context) error {
	samples, err := p.source.ReplicasReady(ctx)
	if err != nil {
		metrics.DataPlaneReadinessScrapeFailures.Inc()
		return fmt.Errorf("observe data-plane readiness: %w", err)
	}
	at := p.now()
	var failures []error
	for _, s := range samples {
		found, err := p.store.RecordReplicasReady(ctx, s.Target.TenantID, s.Target.ProjectID, s.Target.Environment, s.ReplicasReady, at)
		switch {
		case err != nil:
			metrics.DataPlaneReadinessSnapshots.WithLabelValues("failed").Inc()
			failures = append(failures, fmt.Errorf("record readiness for %s: %w", s.Target, err))
		case !found:
			metrics.DataPlaneReadinessSnapshots.WithLabelValues("no_target").Inc()
			p.logger.Warn("data-plane readiness poller: no environment target for sample; its snapshot is not recorded",
				"target", s.Target.String())
		default:
			metrics.DataPlaneReadinessSnapshots.WithLabelValues("recorded").Inc()
		}
	}
	return errors.Join(failures...)
}
