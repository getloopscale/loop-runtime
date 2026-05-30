// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package main is the entrypoint for the LoopZero API server.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"sync"
	"time"

	"github.com/loopzero/loopzero/pkg/enforcement"
	"github.com/loopzero/loopzero/pkg/logging"

	"github.com/loopzero/loopzero/api/internal/admission"
	"github.com/loopzero/loopzero/api/internal/config"
	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/metrics"
	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/placement"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/server"
	"github.com/loopzero/loopzero/api/internal/services"
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

	// pprof profiling — accessible via kubectl port-forward, never via public ingress.
	// Controlled by LOOPZERO_PPROF_ENABLED (default: false). Uses dedicated ServeMux
	// to avoid exposing other handlers registered on DefaultServeMux.
	if pprofEnabled() {
		go func() {
			pprofAddr := os.Getenv("LOOPZERO_PPROF_ADDR")
			if pprofAddr == "" {
				pprofAddr = ":6060"
			}
			mux := http.NewServeMux()
			mux.HandleFunc("/debug/pprof/", pprof.Index)
			mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
			mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
			mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
			mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
			slog.Info("pprof server starting", "addr", pprofAddr)
			// G114 mitigation: explicit Server with ReadHeaderTimeout
			// to block slowloris. WriteTimeout deliberately ZERO —
			// `/debug/pprof/profile` runs 30-60s CPU profiles by
			// design; finite WriteTimeout would truncate them.
			// Same pattern as operators/internal/shared/pprof.go iter 11.
			srv := &http.Server{
				Addr:              pprofAddr,
				Handler:           mux,
				ReadHeaderTimeout: 10 * time.Second,
			}
			if err := srv.ListenAndServe(); err != nil {
				slog.Error("pprof server failed", "error", err)
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

	// WS.1: Track service auth secret age for rotation alerting.
	secretAgeCollector := metrics.NewSecretAgeCollector(cfg.ServiceAuthSecretCreatedAt, logger)
	secretAgeCollector.Start(context.Background())
	defer secretAgeCollector.Stop()

	// T1.3 (Wave 1 Phase A.4): wire auth failure auditor to persist
	// auth rejections to audit_log. Must run AFTER setupDatabase so the
	// writer has a real DB; nil-safe when dbAdapter is unavailable.
	WireAuthFailureAuditor(dbAdapter, logger)

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
		var planLimiter *enforcement.PlanLimiter
		if dbDeps != nil {
			planLimiter = dbDeps.PlanLimiter
		}
		orchDeps = setupOrchestrator(dbAdapter, stores, workflows, cfg, httpTransport, logger, planLimiter)
		ghDeps = setupGitHub(dbAdapter, stores, orchDeps.Orchestrator, cfg, shutdownCtx, logger)
	}

	// CHG-2026-04-30-DELETE-SINGLE-CELL: cellRouter is always DynamicRouter
	// when DB + workflows are wired. The previous PassthroughRouter pre-wire
	// was an artifact of single-cell mode; in startup paths where the router
	// is not yet built (test stubs without DB), routes that need cells get
	// a nil router and must handle it.
	var bgWg sync.WaitGroup
	var cellRouter placement.Router
	var cellStore *db.PostgresCellStore
	var domainHandler *routes.DomainHandler

	if dbAdapter != nil && stores != nil && workflows != nil {
		cellRouter = setupCellRouter(cfg, shutdownCtx, &bgWg, logger)
		cellStore = &db.PostgresCellStore{DB: dbAdapter}

		dh, domainProvisioner, domainHealthChecker, cfZoneClient := setupDomains(stores, workflows, cfg, logger)
		domainHandler = dh

		// MEET-ASYNC-AMENDMENT-2026-05-12-003 Option A (RATIFIED 2026-05-12):
		// run leader election in a goroutine so HTTP server starts immediately.
		// Passive standby pods serve /healthz (probes pass) while waiting for
		// the lock; only the leader runs background reconcile loops.
		// Pre-fix: synchronous setupLeaderElection blocked main() up to 60s,
		// causing 2nd replica liveness/startup probe failure -> crashloop.
		//
		// Iter 19 fix: do NOT defer LeaderConn.Release() inside this goroutine.
		// startLeaderLoops spawns runLeaderHeartbeat (setup_background.go:422)
		// which OWNS the conn for its lifetime. When the wrapping goroutine
		// returns, the deferred Release() yanks the conn out from under the
		// heartbeat -> panic in pgxpool.Conn.QueryRow. The conn is cleaned
		// up at process exit via pgxpool.Close (line 116 main.go).
		bgWg.Add(1)
		go func() {
			defer bgWg.Done()
			leaderResult := setupLeaderElection(dbAdapter.Pool, shutdownCtx, logger)

			if leaderResult.IsLeader {
				startLeaderLoops(shutdownCtx, &bgWg, leaderResult.LeaderConn,
					dbAdapter, stores, workflows, orchDeps, k8sDeps, cellRouter,
					domainProvisioner, domainHealthChecker, cfZoneClient, cfg, logger)
			}
			// Non-leader: setupLeaderElection already started the
			// startNonLeaderReelection goroutine which polls every 60s
			// and os.Exit(0) when lock acquired. LeaderConn lives for
			// the lifetime of that re-election goroutine.
		}()
	}
	defer waitForBackgroundGoroutines(&bgWg, logger)

	buildHandler := setupBuildHandler(dbAdapter, orchDeps, cellStore, cfg, shutdownCtx, logger)

	// Plan T2.2 D5 — wire EnvironmentEnsurer so loopzero deploy --env <novo>
	// auto-creates the Environment CR + namespace + ArgoCD App on first
	// use. Closes F-S1-02 (dogfood 2026-05-15). Wiring requires the
	// TenantCRManager from k8sDeps; both ensurer + BuildHandler are
	// composed here in main so we keep the dependency tree explicit.
	if buildHandler != nil && k8sDeps != nil && k8sDeps.TenantCRMgr != nil {
		lister, adder := services.NewEnvManagerAdapter(
			k8sDeps.TenantCRMgr.AddEnvironment,
			func(ctx context.Context, tenantID, envName string) (any, error) {
				detail, err := k8sDeps.TenantCRMgr.GetEnvironmentDetail(ctx, tenantID, envName)
				if detail == nil {
					return nil, err
				}
				return detail, err
			},
		)
		ensurer := &services.EnvironmentEnsurer{
			Lister: lister,
			Adder:  adder,
			Logger: logger,
		}
		buildHandler = buildHandler.WithEnvironmentEnsurer(ensurer)
		logger.Info("env ensurer wired — deploy --env <novo> will auto-create environments (T2.2 D5)")
	}

	// V9 L20: SecretResyncer wired post-envEnsurer in builds.go. Closes
	// the set-secret-BEFORE-first-deploy race (audit
	// 2026-05-30-V9-L19-99percent). Requires both a value-returning
	// secret store (PostgresSecretStore.ListForResync) and a workload
	// K8s creator (the same SecretK8sSync used by POST /api/v1/secrets).
	if buildHandler != nil && stores != nil && k8sDeps != nil && k8sDeps.SecretK8sSync != nil {
		if pgStore, ok := stores.SecretStore.(*db.PostgresSecretStore); ok {
			resyncer := &services.SecretResyncer{
				Store:  pgStore,
				K8s:    k8sDeps.SecretK8sSync,
				Logger: logger,
			}
			buildHandler = buildHandler.WithSecretResyncer(resyncer)
			logger.Info("secret resyncer wired — deploy will repopulate ExternalSecrets post envEnsurer (V9 L20)")
		}
	}

	healthServices := setupHealthServices(cfg)

	rateLimiter := middleware.NewRateLimiter(60, 1*time.Minute)
	defer rateLimiter.Stop()

	// T2.4 of deep-review-remediation-plan + ADR D4 — pre-auth IP rate limit.
	// Trusted-proxy CIDRs from env (Cloudflare egress in production; empty
	// in dev so X-Forwarded-For is never honored). Limit defaults to
	// 120/min/IP — enough headroom for legitimate batch CLIs, stings on
	// brute-force.
	ipRateLimiter := middleware.NewIPRateLimiter(middleware.IPRateLimitConfig{
		Limit:             120,
		Window:            1 * time.Minute,
		TrustedProxyCIDRs: middleware.ParseTrustedProxyCIDRs(os.Getenv("LOOPZERO_TRUSTED_PROXY_CIDRS")),
	})

	srvCfg := buildServerConfig(cfg, dbAdapter, dbDeps, stores, orchDeps, ghDeps,
		k8sDeps, cellRouter, cellStore, domainHandler, buildHandler,
		healthServices, authDeps.TokenValidator, rateLimiter,
		shutdownCtx, shutdownCancel, logger)
	srvCfg.IPRateLimit = ipRateLimiter

	if err := server.Run(srvCfg); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

// pprofEnabled checks LOOPZERO_PPROF_ENABLED. Default: false (safe for production).
// Set LOOPZERO_PPROF_ENABLED=true explicitly to enable (dev/debug only).
func pprofEnabled() bool {
	v := os.Getenv("LOOPZERO_PPROF_ENABLED")
	return v == "true" || v == "1"
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
	// T1.2b (plan 2026-05-29-sqlc-migration v1.3): sqlc-backed wrapper
	// satisfies db.BuildStore 1:1 — admission querier seam unchanged.
	querier := &buildStoreAdmissionQuerier{store: db.NewBuildStoreSqlc(dbAdapter)}
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
	// Quality-Gate Phase 2: ensure breaker cleanup on shutdown.
	go func() {
		<-shutdownCtx.Done()
		breaker.Stop()
	}()
	if cfg.AdmissionControl {
		logger.Info("pg pool circuit breaker enabled",
			"high_water", 0.90, "low_water", 0.75, "max_conns", cfg.DBMaxConns)
	}
	return buildHandler
}
