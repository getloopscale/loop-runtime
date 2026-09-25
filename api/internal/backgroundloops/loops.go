// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package backgroundloops

import (
	"context"
	"log/slog"
	"time"

	"github.com/getloopscale/loopzero/api/internal/adapters"
	"github.com/getloopscale/loopzero/api/internal/audit"
	"github.com/getloopscale/loopzero/api/internal/builds"
	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	"github.com/getloopscale/loopzero/api/internal/domains"
	"github.com/getloopscale/loopzero/api/internal/domains/cfzone"
	"github.com/getloopscale/loopzero/api/internal/gitops"
	"github.com/getloopscale/loopzero/api/internal/middleware/policy"
	"github.com/getloopscale/loopzero/api/internal/orchestrationbreaker"
	"github.com/getloopscale/loopzero/api/internal/placement"
	"github.com/getloopscale/loopzero/api/internal/release"
	"github.com/getloopscale/loopzero/api/internal/sse"
)

// Loop is one named background loop; Start blocks until ctx is done.
type Loop struct {
	Name  string
	Start func(context.Context)
}

// Deps is everything the loop graph needs. Every store MUST be built on the
// BACKGROUND pool (loopzero_worker): each loop sweeps across tenants, and over the
// request pool (RLS-scoped to one tenant) every sweep reads and deletes
// nothing, with no error.
type Deps struct {
	WorkerPool     *db.PgxAdapter
	BuildStore     db.BuildStore
	ReleaseStore   db.ReleaseStore
	EnvTargetStore *store.PostgresEnvironmentTargetStore
	EventStore     *store.PostgresEventStore
	Locker         *adapters.PoolEnvironmentLocker
	DomainStore    *store.PostgresDomainStore
	CellStore      *store.PostgresCellStore
	AppCR          builds.ApplicationCREnsurer
	Workflow       *orchestrationbreaker.CircuitBreakerWorkflowSubmitter
	Sync           *orchestrationbreaker.CircuitBreakerSyncClient
	GitOps         *gitops.GiteaGitOpsPusher
	Orchestrator   *builds.BuildOrchestrator

	// Optional. A nil CellLister leaves the reconcile loop unfiltered by
	// cell and skips the orphan detector; a nil CFZone leaves the domain
	// provisioner on TXT verification.
	//
	// There is deliberately no KV reconciler and no Cloudflare zone
	// reconciler: platform.KVReconciler needs a KVRoutingSource and
	// domain.DomainCFReconciler a CFZoneReconcilerStore, and no production
	// type implements either. The API used to type-assert its stores to
	// them, the assertions always failed, and neither loop has ever run.
	CellLister deploycontracts.CellLister
	CFZone     *cfzone.Client

	// CellID scopes the build/release loops to one cell (the controller's
	// LOOPZERO_CELL_ID). Empty means every cell, and no per-cell concurrency cap.
	CellID string
}

// Build returns the full loop graph. Both binaries call it; neither wires a
// loop by hand.
func Build(cfg config.Config, d Deps, logger *slog.Logger) []Loop {
	reconcile := newReconcileLoop(cfg, d, logger)
	loops := []Loop{
		{"build-reconcile", reconcile.Start},
		{"build-reaper", newBuildReaper(cfg, d, reconcile, logger).Start},
		{"release-reaper", newReleaseReaper(cfg, d, logger).Start},
		{"release-convergence", newConvergenceLoop(cfg, d, logger).Start},
		{"idempotency-cleaner", policy.NewIdempotencyCleaner(&store.PostgresIdempotencyStore{DB: d.WorkerPool}, logger, time.Hour).Start},
		{"event-pruner", sse.NewEventPruner(d.EventStore, logger, 24*time.Hour, time.Hour).Start},
		{"cell-metrics", placement.NewCellMetricsCollector(d.CellStore, logger, 60*time.Second).Start},
		{"build-gc", builds.NewBuildGC(&store.PostgresBuildGCStore{DB: d.WorkerPool}, logger, time.Hour, 30*24*time.Hour, 10).Start},
		// ADR-0047 audit_log retention (1 year); prunes nightly at 02:00.
		{"audit-log-pruner", audit.NewAuditLogPruner(&store.PostgresAuditLogPruneStore{Pool: d.WorkerPool.Pool}, audit.AuditLogPrunerOpts{Logger: logger}).Start},
		{"domain-provisioner", newDomainProvisioner(cfg, d, logger).Start},
		{"domain-health-checker", newDomainHealthChecker(cfg, d, logger).Start},
	}
	if d.CellLister != nil {
		loops = append(loops, Loop{"orphan-detector", placement.NewOrphanDetector(d.CellLister, d.CellStore, logger, 60*time.Second).Start})
	}
	return loops
}

func newReconcileLoop(cfg config.Config, d Deps, logger *slog.Logger) *builds.BuildReconcileLoop {
	loopCfg := builds.BuildReconcileLoopConfig{
		Interval:           cfg.ReconcileInterval,
		RegistryURL:        cfg.RegistryURL,
		BaseDomain:         cfg.BaseDomain,
		ClusterIssuer:      cfg.ClusterIssuer,
		PrometheusAddress:  cfg.PrometheusAddress,
		MaxBuildsPerTenant: cfg.MaxConcurrentBuildsPerTenant,
		CellID:             d.CellID,
	}
	if d.CellID != "" {
		loopCfg.MaxConcurrent = cfg.MaxConcurrentBuildsPerCell
	}
	loop := builds.NewBuildReconcileLoop(d.BuildStore, d.Workflow, d.Orchestrator, d.Locker,
		d.DomainStore, d.AppCR, d.EventStore, logger, loopCfg)
	if d.CellLister != nil {
		loop.WithCellLister(d.CellLister)
	}
	// J-16: deploy.failed outbox emit on the reconcile-retry FAILED path.
	loop.WithDeployEventStore(store.NewDeployEventStoreSqlc())
	return loop
}

func newBuildReaper(cfg config.Config, d Deps, reconcile *builds.BuildReconcileLoop, logger *slog.Logger) *builds.BuildReaper {
	reaper := builds.NewBuildReaper(d.BuildStore, builds.NewStaleBuildChecker(&store.PostgresStaleScanner{DB: d.WorkerPool}),
		d.Locker, d.Workflow, d.EventStore, logger,
		builds.BuildReaperConfig{
			QueueTimeout:     cfg.BuildQueueTimeout,
			RunningTimeout:   cfg.BuildRunningTimeout,
			ReleasingTimeout: cfg.BuildReleasingTimeout,
			CancelTimeout:    cfg.BuildCancelTimeout,
			Interval:         cfg.BuildReaperInterval,
			CellID:           d.CellID,
		}, reconcile)
	// J-16: deploy.failed outbox emit on the reaper FAILED path.
	reaper.WithDeployEventStore(store.NewDeployEventStoreSqlc())
	return reaper
}

func newReleaseReaper(cfg config.Config, d Deps, logger *slog.Logger) *release.ReleaseReaper {
	return release.NewReleaseReaper(d.ReleaseStore, release.NewStaleReleaseChecker(&store.PostgresStaleScanner{DB: d.WorkerPool}),
		d.Locker, d.EventStore, logger,
		release.ReleaseReaperConfig{
			ReadyTimeout:   cfg.ReleaseReadyTimeout,
			PendingTimeout: cfg.ReleasePendingTimeout,
			Interval:       cfg.ReleaseReaperInterval,
			CellID:         d.CellID,
		})
}

func newConvergenceLoop(cfg config.Config, d Deps, logger *slog.Logger) *release.ReleaseConvergenceLoop {
	// PublicDomain (LOOPZERO_PUBLIC_DOMAIN) is preferred over BaseDomain for the
	// deploy.completed URL so it matches `loopzero status`.
	loop := release.NewReleaseConvergenceLoop(d.ReleaseStore, d.EnvTargetStore, d.Sync, d.Locker,
		d.EventStore, cfg.BaseDomain, cfg.PublicDomain, logger,
		release.ReleaseConvergenceLoopConfig{
			Interval:      10 * time.Second,
			SyncTimeout:   300 * time.Second,
			MaxConcurrent: 10,
			CellID:        d.CellID,
		})
	// J-16: deploy.succeeded outbox writer on the activation path.
	loop.WithDeployEventStore(store.NewDeployEventStoreSqlc())
	return loop
}

func newDomainProvisioner(cfg config.Config, d Deps, logger *slog.Logger) *domains.DomainProvisioner {
	p := domains.NewDomainProvisioner(d.DomainStore, d.ReleaseStore, d.BuildStore, d.GitOps, d.Locker, logger,
		domains.DomainProvisionerConfig{
			Interval:          30 * time.Second,
			BaseDomain:        cfg.BaseDomain,
			RegistryURL:       cfg.RegistryURL,
			ClusterIssuer:     cfg.ClusterIssuer,
			PrometheusAddress: cfg.PrometheusAddress,
		})
	if cfg.CFZoneManagement && d.CFZone != nil {
		p.WithCFZone(&adapters.CFZoneStatusAdapter{Client: d.CFZone})
	}
	return p
}

func newDomainHealthChecker(cfg config.Config, d Deps, logger *slog.Logger) *domains.DomainHealthChecker {
	return domains.NewDomainHealthChecker(d.DomainStore, logger, domains.DomainHealthCheckerConfig{
		CNAMETarget: CNAMETarget(cfg),
		GraceDays:   14,
		StaleDays:   7,
	})
}

// CNAMETarget is the hostname custom domains must CNAME to.
func CNAMETarget(cfg config.Config) string {
	return "cname." + cfg.BaseDomain
}
