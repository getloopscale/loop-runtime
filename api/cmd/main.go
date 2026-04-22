// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package main is the entrypoint for the LoopZero API server.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/loopzero/loopzero/pkg/logging"

	"github.com/loopzero/loopzero/api/internal/admission"
	"github.com/loopzero/loopzero/api/internal/config"
	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/placement"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/server"
	"github.com/loopzero/loopzero/api/internal/tracing"

	"k8s.io/klog/v2"
)

func main() {
	silenceKlog()

	logger := logging.NewLogger("api")
	slog.SetDefault(logger)

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		logger.Error("configuration validation failed", "error", err)
		os.Exit(1)
	}

	tracingShutdown, err := tracing.Init(context.Background(), "loopzero-api")
	if err != nil {
		logger.Warn("tracing disabled", "error", err)
	} else {
		defer func() {
			if err := tracingShutdown(context.Background()); err != nil {
				logger.Error("tracing shutdown failed", "error", err)
			}
		}()
	}

	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())

	if cfg.IsProduction() {
		required := map[string]string{
			"DATABASE_URL":              os.Getenv("DATABASE_URL"),
			"LOOPZERO_ARGO_CALLBACK_SECRET": os.Getenv("LOOPZERO_ARGO_CALLBACK_SECRET"),
			"GITEA_TOKEN":               cfg.GiteaToken,
			"ARGOCD_TOKEN":              cfg.ArgocdToken,
		}
		if err := server.ValidateProductionEnv(logger, required); err != nil {
			os.Exit(1)
		}
	}

	httpTransport := &http.Transport{
		MaxIdleConns: 100, MaxIdleConnsPerHost: 10, IdleConnTimeout: 90 * time.Second,
	}

	authDeps := setupAuth(logger)

	k8sDeps, err := setupK8s(cfg, logger)
	if err != nil {
		logger.Error("failed to create K8s managers", "error", err)
		os.Exit(1)
	}

	dbDeps, err := setupDatabase(cfg, logger)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	var dbAdapter *db.PgxAdapter
	if dbDeps != nil {
		dbAdapter = dbDeps.DBAdapter
		defer dbAdapter.Close()
		defer dbDeps.PoolCollector.Stop()
		if dbDeps.StdDB != nil {
			defer func() { _ = dbDeps.StdDB.Close() }()
		}
	}

	var stores *StoreDeps
	var workflows *WorkflowDeps
	var orchDeps *OrchestratorDeps
	var ghDeps *GitHubDeps
	if dbAdapter != nil {
		stores, err = setupStores(dbAdapter, cfg, logger)
		if err != nil {
			logger.Error("failed to setup stores", "error", err)
			os.Exit(1)
		}
		workflows, err = setupWorkflows(cfg, httpTransport, logger)
		if err != nil {
			logger.Error("failed to setup workflows", "error", err)
			os.Exit(1)
		}
		orchDeps = setupOrchestrator(dbAdapter, stores, workflows, cfg, httpTransport, logger)
		ghDeps = setupGitHub(dbAdapter, stores, orchDeps.Orchestrator, cfg, shutdownCtx, logger)
	}

	var bgWg sync.WaitGroup
	cellRouter := placement.Router(placement.NewPassthroughRouter(
		placement.CellConfig{CellID: placement.DefaultCellID}))
	var cellStore *db.PostgresCellStore
	var domainHandler *routes.DomainHandler

	if dbAdapter != nil && stores != nil && workflows != nil {
		cellRouter = setupCellRouter(cfg, shutdownCtx, &bgWg, logger)
		cellStore = &db.PostgresCellStore{DB: dbAdapter}

		dh, domainProvisioner, domainHealthChecker, cfZoneClient := setupDomains(stores, workflows, cfg, logger)
		domainHandler = dh

		leaderResult := setupLeaderElection(dbAdapter.Pool, shutdownCtx, logger)
		defer leaderResult.LeaderConn.Release()

		if leaderResult.IsLeader {
			startLeaderLoops(shutdownCtx, &bgWg, leaderResult.LeaderConn,
				dbAdapter, stores, workflows, orchDeps, k8sDeps, cellRouter,
				domainProvisioner, domainHealthChecker, cfZoneClient, cfg, logger)
		}
	}
	defer waitForBackgroundGoroutines(&bgWg, logger)

	buildHandler := setupBuildHandler(dbAdapter, orchDeps, cellStore, cfg, shutdownCtx, logger)
	healthServices := setupHealthServices(cfg)

	rateLimiter := middleware.NewRateLimiter(60, 1*time.Minute)
	defer rateLimiter.Stop()

	srvCfg := buildServerConfig(cfg, dbAdapter, dbDeps, stores, orchDeps, ghDeps,
		k8sDeps, cellRouter, cellStore, domainHandler, buildHandler,
		healthServices, authDeps.TokenValidator, rateLimiter,
		shutdownCtx, shutdownCancel, logger)

	if err := server.Run(srvCfg); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func silenceKlog() {
	klogFlags := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(klogFlags)
	_ = klogFlags.Set("logtostderr", "false")
	_ = klogFlags.Set("stderrthreshold", "FATAL")
}

func setupBuildHandler(
	dbAdapter *db.PgxAdapter, orchDeps *OrchestratorDeps, cellStore *db.PostgresCellStore,
	cfg config.Config, shutdownCtx context.Context, logger *slog.Logger,
) *routes.BuildHandler {
	if orchDeps == nil {
		return nil
	}
	buildHandler := routes.NewBuildHandler(orchDeps.BuildOrchestrator, logger)
	if dbAdapter == nil {
		return buildHandler
	}
	querier := &buildStoreAdmissionQuerier{store: db.NewPostgresBuildStore(dbAdapter)}
	admissionCfg := admission.DefaultConfig()
	if cfg.MaxConcurrentBuildsPerCell > 0 {
		admissionCfg.MaxConcurrentPerCell = cfg.MaxConcurrentBuildsPerCell
	}
	if cfg.MaxConcurrentBuildsPerTenant > 0 {
		admissionCfg.MaxConcurrentPerTenant = cfg.MaxConcurrentBuildsPerTenant
	}
	gate := admission.NewBuildGate(querier, admissionCfg, cfg.AdmissionControl)
	buildHandler = buildHandler.WithAdmission(admission.NewRouteAdapter(gate), cellStore)
	if cfg.AdmissionControl {
		logger.Info("build admission gate enabled",
			"max_per_cell", admissionCfg.MaxConcurrentPerCell,
			"max_per_tenant", admissionCfg.MaxConcurrentPerTenant)
	}
	breaker := admission.NewPGPoolBreaker(dbAdapter.Pool, admission.DefaultBreakerConfig(cfg.DBMaxConns), logger)
	breaker.Start(shutdownCtx)
	if cfg.AdmissionControl {
		logger.Info("pg pool circuit breaker enabled",
			"high_water", 0.90, "low_water", 0.75, "max_conns", cfg.DBMaxConns)
	}
	return buildHandler
}
