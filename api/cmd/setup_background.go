// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/getloopscale/loopzero/api/internal/backgroundloops"
	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	"github.com/getloopscale/loopzero/api/internal/domains/cfzone"
	"github.com/getloopscale/loopzero/api/internal/domains/domainsadapter"
	"github.com/getloopscale/loopzero/api/internal/domains/domainshttp"
	"github.com/getloopscale/loopzero/api/internal/environments"
	"github.com/getloopscale/loopzero/api/internal/placement"
	"github.com/getloopscale/loopzero/api/internal/ports"
)

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

// setupDomains creates the custom-domain HTTP handler and the Cloudflare zone
// client. The provisioner and health checker are background loops and are
// built by backgroundloops.Build with the rest of the loop graph.
func setupDomains(stores *StoreDeps, cfg config.Config, logger *slog.Logger) (*domainshttp.DomainHandler, *cfzone.Client) {
	domainHandler := domainshttp.NewDomainHandler(stores.DomainStore, &domainsadapter.DNSTXTVerifier{},
		ports.NewValidator(cfg.BaseDomain, nil), backgroundloops.CNAMETarget(cfg))

	cfZoneClient := cfzone.NewClient(cfg.CFAccountID, cfg.CFAPIToken)
	if cfg.CFZoneManagement && cfZoneClient != nil {
		logger.Info("custom domains: CF Zone Management enabled (v2)")
		domainHandler.WithCFZone(&domainsadapter.CFZoneAdapter{Client: cfZoneClient})
	} else {
		logger.Info("custom domains: TXT verification mode (v1)")
	}
	return domainHandler, cfZoneClient
}

// setupReadinessSource builds the data-plane readiness poller's Prometheus
// source when LOOPZERO_DATA_PLANE_POLLER_ENABLED is on, and refuses to start on a
// source it cannot build: a poller silently absent leaves every dashboard
// badge "Unknown" with no error anywhere.
func setupReadinessSource(cfg config.Config, logger *slog.Logger) environments.ReplicasReadySource {
	source, err := backgroundloops.NewReadinessSource(cfg)
	if err != nil {
		logger.Error("refusing to start", "error", err)
		os.Exit(1)
	}
	if source != nil {
		logger.Info("data-plane readiness poller enabled", "interval", cfg.DataPlanePollerInterval)
	}
	return source
}

// startLeaderLoops starts the background loop graph (backgroundloops.Build)
// over the BACKGROUND pool, each loop bound to ctx: they stop when the
// election cancels it (lease fenced or shutdown). `bgAdapter` and `stores`
// MUST come from the background pool (loopzero_worker); see backgroundloops.Deps.
func startLeaderLoops(
	ctx context.Context,
	bgWg *sync.WaitGroup,
	bgAdapter *db.PgxAdapter,
	stores *StoreDeps,
	workflows *WorkflowDeps,
	orchDeps *OrchestratorDeps,
	k8sDeps *K8sDeps,
	cellRouter placement.Router,
	cfZoneClient *cfzone.Client,
	readinessSource environments.ReplicasReadySource,
	cfg config.Config,
	logger *slog.Logger,
) {
	deps := backgroundloops.Deps{
		WorkerPool:      bgAdapter,
		BuildStore:      stores.BuildStore,
		ReleaseStore:    stores.ReleaseStore,
		EnvTargetStore:  stores.EnvTargetStore,
		EventStore:      stores.PgEventStore,
		Locker:          stores.Locker,
		DomainStore:     stores.DomainStore,
		CellStore:       stores.CellStore,
		AppCR:           k8sDeps.AppCRMgr,
		Workflow:        workflows.WorkflowSubmitter,
		Sync:            workflows.SyncClient,
		GitOps:          workflows.GitOpsPusher,
		Orchestrator:    orchDeps.Orchestrator,
		CFZone:          cfZoneClient,
		ReadinessSource: readinessSource,
	}
	if lister, ok := cellRouter.(deploycontracts.CellLister); ok {
		deps.CellLister = lister
	}
	loops := backgroundloops.Build(cfg, deps, logger)
	bgWg.Add(len(loops))
	for _, loop := range loops {
		l := loop // capture for goroutine
		go func() {
			defer bgWg.Done()
			l.Start(ctx)
		}()
	}
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
