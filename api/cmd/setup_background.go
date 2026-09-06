// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/getloopscale/loopzero/api/internal/services/build"

	"github.com/getloopscale/loopzero/api/internal/services/release"

	"github.com/getloopscale/loopzero/api/internal/services/deploycontracts"

	"github.com/getloopscale/loopzero/api/internal/services/platform"

	"github.com/getloopscale/loopzero/api/internal/services/domain"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loopzero/api/internal/cfkv"
	"github.com/getloopscale/loopzero/api/internal/cfzone"
	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/placement"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/routes/domains"
	"github.com/getloopscale/loopzero/api/internal/sse"
)

// LeaderElectionResult holds leader election state.
type LeaderElectionResult struct {
	IsLeader   bool
	LeaderConn *pgxpool.Conn
}

const leaderLockID int64 = 0x7468656F6C656164 // "loopzerolead" in hex

// setupLeaderElection acquires the leader advisory lock.
// Caller must defer LeaderConn.Release() on shutdown.
func setupLeaderElection(
	pool *pgxpool.Pool,
	shutdownCtx context.Context,
	logger *slog.Logger,
) *LeaderElectionResult {
	leaderConn, err := pool.Acquire(context.Background())
	if err != nil {
		logger.Error("failed to acquire database connection for leader election", "error", err)
		os.Exit(1)
	}

	var isLeader bool
	logger.Info("starting leader election", "lockID", leaderLockID, "maxAttempts", 12)
	for attempt := 0; attempt < 12; attempt++ {
		if err := leaderConn.QueryRow(context.Background(),
			"SELECT pg_try_advisory_lock($1)", leaderLockID).Scan(&isLeader); err != nil {
			logger.Error("failed to acquire leader lock", "error", err)
			os.Exit(1)
		}
		if isLeader {
			break
		}
		logger.Info("leader lock held by another instance, retrying...", "attempt", attempt+1, "max", 12)
		time.Sleep(5 * time.Second)
	}

	if isLeader {
		metrics.LeaderStatus.Set(1)
		logger.Info("leader lock acquired — background loops will start on this replica")
	} else {
		metrics.LeaderStatus.Set(0)
		logger.Warn("another API instance holds the leader lock — background loops will NOT start on this replica",
			"hint", "The API can serve HTTP requests but reconcile/reaper loops are disabled.")
		startNonLeaderReelection(leaderConn, shutdownCtx, logger)
	}

	return &LeaderElectionResult{IsLeader: isLeader, LeaderConn: leaderConn}
}

// startNonLeaderReelection periodically retries leader acquisition for non-leader pods.
func startNonLeaderReelection(leaderConn *pgxpool.Conn, shutdownCtx context.Context, logger *slog.Logger) {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-shutdownCtx.Done():
				return
			case <-ticker.C:
				var acquired bool
				// Derive from shutdownCtx so shutdown cancels in-flight
				// pg_try_advisory_lock attempts. Was context.Background()
				// which leaked the attempt past shutdown signal.
				reelCtx, reelCancel := context.WithTimeout(shutdownCtx, 5*time.Second)
				if err := leaderConn.QueryRow(reelCtx,
					"SELECT pg_try_advisory_lock($1)", leaderLockID).Scan(&acquired); err != nil {
					reelCancel()
					continue
				}
				reelCancel()
				if acquired {
					logger.Info("non-leader pod acquired leader lock — restarting to enable background loops")
					metrics.LeaderStatus.Set(1)
					os.Exit(0)
				}
			}
		}
	}()
}

// setupCellRouter creates the DynamicRouter — the ONE router used in
// production. There is no single-cell mode, no PassthroughRouter, no
// StaticRouter fallback. If ClusterCredential discovery returns 0 cells
// the API fails fast on boot — there is nowhere to route requests.
//
// CHG-2026-04-30-DELETE-SINGLE-CELL: removed cfg.SingleCell + cfg.DynamicRouter
// toggles. Single-cell is the degenerate case of multi-cell with 1 CC
// discovered. N CCs = router distributes; pre-release governance:
// no feature flags, no rollback paths.
func setupCellRouter(
	cfg config.Config,
	shutdownCtx context.Context,
	bgWg *sync.WaitGroup,
	logger *slog.Logger,
) placement.Router {
	k8sCellStore, k8sErr := placement.NewK8sCellStore(nil, "loopzero-system")
	if k8sErr != nil {
		logger.Error("cell router: K8sCellStore init failed — fatal, no fallback by design",
			"error", k8sErr)
		os.Exit(1)
	}
	dynRouter := placement.NewDynamicRouter(k8sCellStore, 60*time.Second, logger)
	cells, loadErr := k8sCellStore.LoadCells(context.Background())
	if loadErr != nil {
		logger.Error("cell router: initial cell load failed — fatal, no fallback by design",
			"error", loadErr)
		os.Exit(1)
	}
	if len(cells) == 0 {
		logger.Error("cell router: 0 ClusterCredentials discovered — fatal, no fallback by design",
			"hint", "create at least one ClusterCredential CR with status.connected=true in loopzero-system",
			"hint_docs", "create at least one ClusterCredential CR with status.connected=true")
		os.Exit(1)
	}
	bgWg.Add(1)
	go func() {
		defer bgWg.Done()
		dynRouter.Start(shutdownCtx)
	}()
	logger.Info("cell router: DynamicRouter active", "cells_discovered", len(cells))
	return dynRouter
}

// DomainDeps holds domain-related components created during background setup.
type DomainDeps struct {
	DomainHandler *domains.DomainHandler
}

// setupDomains creates the domain handler, provisioner, and health checker.
// The provisioner and health checker are started as background loops only when isLeader is true.
func setupDomains(
	stores *StoreDeps,
	workflows *WorkflowDeps,
	cfg config.Config,
	logger *slog.Logger,
) (*domains.DomainHandler, *domain.DomainProvisioner, *domain.DomainHealthChecker, *cfzone.Client) {
	cnameTarget := "cname." + cfg.BaseDomain
	domainValidator := ports.NewValidator(cfg.BaseDomain, nil)
	dnsVerifier := &ports.DNSVerifier{}
	domainHandler := domains.NewDomainHandler(stores.DomainStore, dnsVerifier, domainValidator, cnameTarget)

	cfZoneClient := cfzone.NewClient(cfg.CFAccountID, cfg.CFAPIToken)
	if cfg.CFZoneManagement && cfZoneClient != nil {
		logger.Info("custom domains: CF Zone Management enabled (v2)")
		domainHandler.WithCFZone(&cfZoneAdapter{client: cfZoneClient})
	} else {
		logger.Info("custom domains: TXT verification mode (v1)")
	}

	domainProvisioner := domain.NewDomainProvisioner(
		stores.DomainStore, stores.ReleaseStore, stores.BuildStore,
		workflows.GitOpsPusher, stores.Locker, logger,
		domain.DomainProvisionerConfig{
			Interval:          30 * time.Second,
			BaseDomain:        cfg.BaseDomain,
			RegistryURL:       cfg.RegistryURL,
			ClusterIssuer:     cfg.ClusterIssuer,     // B14 (MEET-2026-04-23-058)
			PrometheusAddress: cfg.PrometheusAddress, // argo-rollouts-progressive-delivery-plan T1.2 (D6)
		},
	)
	if cfg.CFZoneManagement && cfZoneClient != nil {
		domainProvisioner.WithCFZone(&cfZoneStatusAdapter{client: cfZoneClient})
	}

	domainHealthChecker := domain.NewDomainHealthChecker(
		stores.DomainStore, logger, domain.DomainHealthCheckerConfig{
			CNAMETarget: cnameTarget,
			GraceDays:   14,
			StaleDays:   7,
		},
	)

	return domainHandler, domainProvisioner, domainHealthChecker, cfZoneClient
}

// bgLoop describes a named background loop with an optional condition guard.
type bgLoop struct {
	name  string
	start func(context.Context)
}

// createCoreLoops builds the list of always-on background loops.
//
// `bgAdapter` and `stores` MUST come from the BACKGROUND pool (loopzero_worker),
// never from the request pool. Every loop here sweeps across tenants by
// definition, and the request role holds no cross-tenant policy: over that pool
// each sweep reads and deletes NOTHING — no error, no log, no metric. The boot
// guard in setup_background_database.go refuses to hand over a pool without the
// named reach, so this contract fails loudly instead of quietly.
func createCoreLoops(
	bgAdapter *db.PgxAdapter,
	stores *StoreDeps,
	workflows *WorkflowDeps,
	orchDeps *OrchestratorDeps,
	k8sDeps *K8sDeps,
	cellRouter placement.Router,
	domainProvisioner *domain.DomainProvisioner,
	domainHealthChecker *domain.DomainHealthChecker,
	cfg config.Config,
	logger *slog.Logger,
) []bgLoop {
	buildStore := stores.BuildStore
	releaseStore := stores.ReleaseStore
	envTargetStore := stores.EnvTargetStore
	pgEventStore := stores.PgEventStore

	reconcileLoop := build.NewBuildReconcileLoop(
		buildStore,
		workflows.WorkflowSubmitter,
		orchDeps.Orchestrator,
		stores.Locker,
		stores.DomainStore,
		k8sDeps.AppCRMgr,
		pgEventStore,
		logger,
		build.BuildReconcileLoopConfig{
			Interval:           cfg.ReconcileInterval,
			RegistryURL:        cfg.RegistryURL,
			BaseDomain:         cfg.BaseDomain,
			ClusterIssuer:      cfg.ClusterIssuer,     // B14 (MEET-2026-04-23-058)
			PrometheusAddress:  cfg.PrometheusAddress, // argo-rollouts-progressive-delivery-plan T1.2 (D6)
			MaxBuildsPerTenant: cfg.MaxBuildsPerTenant,
		},
	)
	if lister, ok := cellRouter.(deploycontracts.CellLister); ok {
		reconcileLoop.WithCellLister(lister)
	}
	// J-16 slice 3 (MEET-2026-07-04-001): deploy.failed outbox emit on the
	// reconcile-retry FAILED path.
	reconcileLoop.WithDeployEventStore(store.NewDeployEventStoreSqlc())

	reaper := build.NewBuildReaper(
		buildStore,
		&build.PostgresStaleBuildChecker{DB: bgAdapter},
		stores.Locker,
		workflows.WorkflowSubmitter,
		stores.EventStore,
		logger,
		build.BuildReaperConfig{
			QueueTimeout:     cfg.BuildQueueTimeout,
			RunningTimeout:   cfg.BuildRunningTimeout,
			ReleasingTimeout: cfg.BuildReleasingTimeout,
			CancelTimeout:    cfg.BuildCancelTimeout,
			Interval:         cfg.BuildReaperInterval,
		},
		reconcileLoop,
	)
	// J-16 slice 3 (MEET-2026-07-04-001): deploy.failed outbox emit on the
	// reaper FAILED path (cancel-timeout → CANCELLED does NOT emit).
	reaper.WithDeployEventStore(store.NewDeployEventStoreSqlc())

	releaseReaper := release.NewReleaseReaper(
		releaseStore,
		&release.PostgresStaleReleaseChecker{DB: bgAdapter},
		stores.Locker,
		pgEventStore,
		logger,
		release.ReleaseReaperConfig{
			ReadyTimeout:   cfg.ReleaseReadyTimeout,
			PendingTimeout: cfg.ReleasePendingTimeout,
			Interval:       cfg.ReleaseReaperInterval,
		},
	)

	// release-blockers-2026-06-25-set v1.1 #61: cfg.PublicDomain (canonical
	// LOOPZERO_PUBLIC_DOMAIN) preferred over BaseDomain for deploy.completed
	// URL so it matches `loopzero status`.
	releaseConvergenceLoop := release.NewReleaseConvergenceLoop(
		releaseStore, envTargetStore, workflows.SyncClient, stores.Locker,
		stores.EventStore, cfg.BaseDomain, cfg.PublicDomain, logger,
		release.ReleaseConvergenceLoopConfig{
			Interval:      10 * time.Second,
			SyncTimeout:   300 * time.Second,
			MaxConcurrent: 10,
		},
	)
	// J-16 slice 3 — wire the deploy.succeeded transactional-outbox writer so
	// the convergence-loop activation path stages deploy_events on the tx.
	releaseConvergenceLoop.WithDeployEventStore(store.NewDeployEventStoreSqlc())

	idempotencyCleaner := platform.NewIdempotencyCleaner(
		&store.PostgresIdempotencyStore{DB: bgAdapter}, logger, 1*time.Hour,
	)
	eventPruner := sse.NewEventPruner(pgEventStore, logger, 24*time.Hour, 1*time.Hour)
	cellMetrics := placement.NewCellMetricsCollector(stores.CellStore, logger, 60*time.Second)
	buildGC := build.NewBuildGC(
		&store.PostgresBuildGCStore{DB: bgAdapter}, logger, 1*time.Hour, 30*24*time.Hour, 10,
	)
	// Quality-Gate Phase 2: wire AuditLogPruner — ADR-0047 audit_log retention
	// (1 year default). Was implemented but never wired, leaving audit_log to
	// grow unbounded. PruneOnce runs nightly at 02:00 local via Start().
	auditLogPruner := platform.NewAuditLogPruner(bgAdapter.Pool, platform.AuditLogPrunerOpts{
		Logger: logger,
	})

	loops := []bgLoop{
		{"build-reconcile", reconcileLoop.Start},
		{"build-reaper", reaper.Start},
		{"release-reaper", releaseReaper.Start},
		{"release-convergence", releaseConvergenceLoop.Start},
		{"idempotency-cleaner", idempotencyCleaner.Start},
		{"event-pruner", eventPruner.Start},
		{"cell-metrics", cellMetrics.Start},
		{"domain-provisioner", domainProvisioner.Start},
		{"domain-health-checker", domainHealthChecker.Start},
		{"build-gc", buildGC.Start},
		{"audit-log-pruner", auditLogPruner.Start},
	}

	// OrphanDetector (conditional on CellLister + OrphanedTenantQuerier)
	if lister, ok := cellRouter.(deploycontracts.CellLister); ok {
		if querier, ok2 := interface{}(stores.CellStore).(platform.OrphanedTenantQuerier); ok2 {
			orphanDetector := platform.NewOrphanDetector(lister, querier, logger, 60*time.Second)
			loops = append(loops, bgLoop{"orphan-detector", orphanDetector.Start})
		}
	}

	return loops
}

// createOptionalLoops builds the list of conditional background loops (KV, CF reconcilers).
func createOptionalLoops(
	stores *StoreDeps,
	workflows *WorkflowDeps,
	cfZoneClient *cfzone.Client,
	cfg config.Config,
	logger *slog.Logger,
) []bgLoop {
	var loops []bgLoop

	// KVReconciler (optional)
	if workflows.KVClient != nil {
		if kvC, ok := workflows.KVClient.(*cfkv.Client); ok {
			if kvSource, ok2 := interface{}(stores.BuildStore).(platform.KVRoutingSource); ok2 {
				kvReconciler := platform.NewKVReconciler(kvC, kvSource, logger, 5*time.Minute)
				loops = append(loops, bgLoop{"kv-reconciler", kvReconciler.Start})
			}
		}
	}

	// DomainCFReconciler (optional)
	if cfg.CFZoneManagement && cfZoneClient != nil {
		if cfStore, ok := interface{}(stores.DomainStore).(domain.CFZoneReconcilerStore); ok {
			domainCFReconciler := domain.NewDomainCFReconciler(cfStore, cfZoneClient, logger, 15*time.Minute)
			loops = append(loops, bgLoop{"domain-cf-reconciler", domainCFReconciler.Start})
		}
	}

	return loops
}

// runLeaderHeartbeat monitors the leader advisory lock and exits if lost.
func runLeaderHeartbeat(
	shutdownCtx context.Context,
	leaderConn *pgxpool.Conn,
	logger *slog.Logger,
) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-shutdownCtx.Done():
			return
		case <-ticker.C:
			var stillLeader bool
			heartbeatCtx, heartbeatCancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := leaderConn.QueryRow(heartbeatCtx,
				"SELECT pg_try_advisory_lock($1)", leaderLockID).Scan(&stillLeader); err != nil {
				heartbeatCancel()
				logger.Error("leader heartbeat: lock check failed — exiting", "error", err)
				metrics.LeaderStatus.Set(0)
				os.Exit(1)
			}
			heartbeatCancel()
			if !stillLeader {
				logger.Error("leader heartbeat: lock LOST — exiting for re-election")
				metrics.LeaderStatus.Set(0)
				os.Exit(1)
			}
			metrics.LeaderStatus.Set(1)
		}
	}
}

// startLeaderLoops starts every leader-only loop over the BACKGROUND pool.
// See createCoreLoops for why it must not be the request pool.
func startLeaderLoops(
	shutdownCtx context.Context,
	bgWg *sync.WaitGroup,
	leaderConn *pgxpool.Conn,
	bgAdapter *db.PgxAdapter,
	stores *StoreDeps,
	workflows *WorkflowDeps,
	orchDeps *OrchestratorDeps,
	k8sDeps *K8sDeps,
	cellRouter placement.Router,
	domainProvisioner *domain.DomainProvisioner,
	domainHealthChecker *domain.DomainHealthChecker,
	cfZoneClient *cfzone.Client,
	cfg config.Config,
	logger *slog.Logger,
) {
	if cfg.DisableLegacyLoops {
		logger.Info("LOOPZERO_DISABLE_LEGACY_LOOPS=true — loops will not run here; controller binary owns them")
		metrics.LeaderStatus.Set(0)
		return
	}

	coreLoops := createCoreLoops(bgAdapter, stores, workflows, orchDeps, k8sDeps, cellRouter, domainProvisioner, domainHealthChecker, cfg, logger)
	optionalLoops := createOptionalLoops(stores, workflows, cfZoneClient, cfg, logger)
	allLoops := append(coreLoops, optionalLoops...)

	// Start all loops + 1 slot for leader heartbeat
	bgWg.Add(len(allLoops) + 1)
	for _, loop := range allLoops {
		l := loop // capture for goroutine
		go func() {
			defer bgWg.Done()
			l.start(shutdownCtx)
		}()
	}

	// Leader heartbeat
	go func() {
		defer bgWg.Done()
		runLeaderHeartbeat(shutdownCtx, leaderConn, logger)
	}()
}

// waitForBackgroundGoroutines waits up to 30s for background goroutines to stop.
func waitForBackgroundGoroutines(bgWg *sync.WaitGroup, logger *slog.Logger) {
	waitDone := make(chan struct{})
	go func() {
		bgWg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
		logger.Info("all background goroutines stopped")
	case <-time.After(30 * time.Second):
		logger.Warn("background goroutines did not stop within 30s, proceeding with shutdown")
	}
}
