// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package main is the entrypoint for the LoopZero API server.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/getloopscale/loopzero/api/internal/adapters"

	"github.com/getloopscale/loopzero/api/internal/routes/builds"
	"github.com/getloopscale/loopzero/api/internal/routes/promote"

	"github.com/getloopscale/loopzero/api/internal/services/platform"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/pkg/logging"

	"github.com/getloopscale/loopzero/api/internal/buildplane/admission"
	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/middleware/clientip"
	"github.com/getloopscale/loopzero/api/internal/middleware/policy"
	ratelimit "github.com/getloopscale/loopzero/api/internal/middleware/ratelimit"
	"github.com/getloopscale/loopzero/api/internal/placement"
	"github.com/getloopscale/loopzero/api/internal/routes/domains"
	"github.com/getloopscale/loopzero/api/internal/server"
	"github.com/getloopscale/loopzero/api/internal/tracing"

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

	startPprofServer(cfg, logger)

	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()

	if err := validateProductionEnv(cfg, logger); err != nil {
		os.Exit(1)
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
	envEnsurer := newEnvironmentEnsurer(cfg, k8sDeps, logger)

	dbDeps, err := setupDatabase(cfg, logger)
	if err != nil {
		logDatabaseSetupFailure(err, logger)
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

	// Trusted-proxy CIDRs from env (Cloudflare egress in production; empty
	// in dev so X-Forwarded-For is never honored). One policy for every
	// consumer of the client IP: the edge rate limit and the auth audit
	// throttle key on the same address.
	trustedProxyCIDRs := clientip.ParseTrustedProxyCIDRs(cfg.TrustedProxyCIDRs)
	logger.Info("trusted proxy CIDRs loaded", "count", len(trustedProxyCIDRs))

	// T1.3 (Wave 1 Phase A.4): the auth failure auditor persists auth
	// rejections to audit_log, so it is built AFTER setupDatabase; nil when
	// dbAdapter is unavailable. It completes the authentication config read
	// in setupAuth, and the Authenticator is built once from both.
	authDeps.Config.Auditor = buildAuthFailureAuditor(dbAdapter, logger, clientip.NewResolver(trustedProxyCIDRs))
	authenticator := auth.NewAuthenticator(authDeps.Config)

	var stores *StoreDeps

	var bgStores *StoreDeps

	var bgAdapter *db.PgxAdapter
	var workflows *WorkflowDeps
	var orchDeps *OrchestratorDeps
	var ghDeps *GitHubDeps
	// T2.5 wire-up (plan 2026-06-10-j22-openbao-transit-eso-sync v1.2):
	// construct the OpenBao transit client BEFORE setupStores so the
	// secret store boots with vault-backed dispatch. Returns nil when
	// CryptoMode != "transit" (legacy AES/plaintext path). D6 ADR
	// fail-loud: construction failure here is fatal — operator MUST
	// fix the config before the API binds. config.Validate() already
	// caught missing env vars; reaching here with a vault.Transit error
	// means the constructor itself failed (bad URL, SPKI parse error).
	transit, terr := setupTransitClient(cfg, logger)
	if terr != nil {
		logger.Error("openbao transit client constructor failed", "error", terr)
		os.Exit(1)
	}
	if dbAdapter != nil {
		stores, err = setupStores(dbAdapter, cfg, transit, logger)
		if err != nil {
			logger.Error("failed to setup stores", "error", err)
			os.Exit(1)
		}
		// The background loops do NOT use the request pool. That pool is
		// deliberately unable to see another tenant (AssertRequestPoolIsolated
		// refuses to boot if it can), and sweeping across tenants is exactly
		// their job — over that pool, every sweep would read and delete
		// NOTHING, with no error and no log. See setup_background_database.go.
		bgAdapter, err = setupBackgroundDatabase(cfg, logger)
		if err != nil {
			logger.Error("failed to connect the background database pool", "error", err)
			os.Exit(1)
		}
		defer bgAdapter.Close()
		bgStores, err = setupStores(bgAdapter, cfg, transit, logger)
		if err != nil {
			logger.Error("failed to setup background stores", "error", err)
			os.Exit(1)
		}
		// The project cap is PER ACCOUNT and counts across the Account's tenants.
		// The request pool sees one tenant since migration 000062, so both
		// project-creating stores (this API store and the orchestrator's deploy
		// store) count through the background pool. Measured: an account with 2
		// tenants and 1 project each read 1 through the request pool and 2
		// through loopzero_worker. See internal/db/store/project_wrapper.go.
		projectCap := newProjectCapEnforcement(dbDeps.PlanLimiter, bgAdapter)
		wireProjectCap(dbDeps.PostgresProjects, projectCap)
		workflows, err = setupWorkflows(cfg, httpTransport, logger)
		if err != nil {
			logger.Error("failed to setup workflows", "error", err)
			os.Exit(1)
		}
		orchDeps = setupOrchestrator(dbAdapter, stores, workflows, cfg, httpTransport, logger, projectCap, envEnsurer)
		ghDeps = setupGitHub(dbAdapter, stores, orchDeps.Orchestrator, k8sDeps, cfg, shutdownCtx, logger)
	}

	// Plan J-17 T1.3 — start the Argo step watcher in a background
	// goroutine so build.step.* events flow into the SSE journal as the
	// build progresses. Non-fatal if the build cluster is unreachable
	// (e.g., local dev without all clusters up); the watcher logs and
	// disables itself. Cancelled with shutdownCtx on graceful stop.
	if stores != nil {
		startArgoStepWatcher(shutdownCtx, cfg, stores.EventStore, logger)
	}

	// CHG-2026-04-30-DELETE-SINGLE-CELL: cellRouter is always DynamicRouter
	// when DB + workflows are wired. The previous PassthroughRouter pre-wire
	// was an artifact of single-cell mode; in startup paths where the router
	// is not yet built (test stubs without DB), routes that need cells get
	// a nil router and must handle it.
	var bgWg sync.WaitGroup
	var cellRouter placement.Router
	var cellStore *store.PostgresCellStore
	var domainHandler *domains.DomainHandler

	if dbAdapter != nil && stores != nil && workflows != nil {
		cellRouter = setupCellRouter(cfg, shutdownCtx, &bgWg, logger)
		cellStore = &store.PostgresCellStore{DB: dbAdapter}

		dh, cfZoneClient := setupDomains(stores, cfg, logger)
		domainHandler = dh

		startLoopsElection(shutdownCtx, &bgWg, cfg, bgAdapter, logger, func(loopsCtx context.Context) {
			startLeaderLoops(loopsCtx, &bgWg,
				bgAdapter, bgStores, workflows, orchDeps, k8sDeps, cellRouter,
				cfZoneClient, cfg, logger)
		})
	}
	defer waitForBackgroundGoroutines(&bgWg, logger)

	buildHandler := setupBuildHandler(dbAdapter, orchDeps, cellStore, cfg, shutdownCtx, logger)

	// The same ensurer serves deploy and project creation (and promote,
	// wired in setupOrchestrator). The build handler exists only with the
	// orchestrator, i.e. with a database.
	var projectEnvEnsurer promote.EnvironmentEnsurer
	if buildHandler != nil && envEnsurer != nil {
		buildHandler = buildHandler.WithEnvironmentEnsurer(envEnsurer)
		projectEnvEnsurer = envEnsurer
		logger.Info("env ensurer wired: deploy --env, project create and promote --to create missing environments")
	}

	buildHandler = withSecretResyncer(buildHandler, stores, k8sDeps, logger)

	healthServices := setupHealthServices(cfg)

	rateLimiter := ratelimit.NewRateLimiter(60, 1*time.Minute)

	// T2.4 of deep-review-remediation-plan + ADR D4 — pre-auth IP rate limit.
	// Trusted-proxy CIDRs from env (Cloudflare egress in production; empty
	// in dev so X-Forwarded-For is never honored). Limit defaults to
	// 120/min/IP — enough headroom for legitimate batch CLIs, stings on
	// brute-force.
	ipRateLimiter := ratelimit.NewIPRateLimiter(ratelimit.IPRateLimitConfig{
		Limit:             120,
		Window:            1 * time.Minute,
		TrustedProxyCIDRs: trustedProxyCIDRs,
	})

	srvCfg := buildServerConfig(serverWiring{
		cfg: cfg, dbAdapter: dbAdapter, dbDeps: dbDeps, stores: stores,
		orchDeps: orchDeps, ghDeps: ghDeps, k8sDeps: k8sDeps,
		cellRouter: cellRouter, cellStore: cellStore,
		domainHandler: domainHandler, buildHandler: buildHandler,
		healthServices: healthServices, tokenValidator: authDeps.TokenValidator,
		rateLimiter: rateLimiter, shutdownCtx: shutdownCtx, shutdownCancel: shutdownCancel,
		logger: logger,
	})
	srvCfg.Guards.IPRateLimit = ipRateLimiter
	srvCfg.Auth = authenticator
	srvCfg.Admin.APIEnabled = os.Getenv("LOOPZERO_ADMIN_API") == "true"
	srvCfg.Maintenance = policy.ParseMaintenanceSetting(os.Getenv("LOOPZERO_MAINTENANCE_MODE"))

	// M3 of qa-readiness-roadmap-2026-06-23 — J11D7 LOCKED ADR:
	// per-(tenant, domain) rate-limit for POST /api/v1/domains/{domain}/verify
	// (1 reverify per 30 seconds). Composition over duplication: VerifyRateLimiter
	// wraps RateLimiter internally — see plan m3-verify-rate-limit-middleware
	// ADR D1.
	verifyRateLimiter := ratelimit.NewVerifyRateLimiter(1, 30*time.Second)
	srvCfg.Guards.VerifyRateLimiter = verifyRateLimiter

	srvCfg.Secrets.FetchHandler = newSecretFetchBroker(cfg, transit, dbAdapter, cellStore, stores, logger)
	// T4.1 of plan 2026-06-09-m2-operator-canonical-fix v1.4: ADR D4 —
	// every project gets a `production` env auto-created on POST
	// /api/v1/projects. Same EnvironmentEnsurer that powers loopzero deploy
	// --env <novo> (T2.2 D5); zero new infrastructure. nil-safe — when
	// projectEnvEnsurer is nil (k8sDeps unavailable or test boot), the
	// project Create handler falls back to pre-T4.1 behavior.
	srvCfg.ProjectEnvEnsurer = projectEnvEnsurer

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
	dbAdapter *db.PgxAdapter, orchDeps *OrchestratorDeps, cellStore *store.PostgresCellStore,
	cfg config.Config, shutdownCtx context.Context, logger *slog.Logger,
) *builds.BuildHandler {
	if orchDeps == nil {
		return nil
	}
	buildHandler := builds.NewBuildHandler(orchDeps.BuildOrchestrator, logger)
	if dbAdapter == nil {
		return buildHandler
	}
	// T1.2b (plan 2026-05-29-sqlc-migration v1.3): sqlc-backed wrapper
	// satisfies db.BuildStore 1:1 — admission querier seam unchanged.
	querier := &adapters.BuildStoreAdmissionQuerier{Store: store.NewBuildStoreSqlc(dbAdapter)}
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
	// B-054: the Postgres pool breaker used to be built HERE and never consulted — it protects
	// the whole API, not the builds handler, and building it at this point left it trapped in a
	// function that returns only the handler. It moved to `setup_server.go`, where the server
	// config is assembled and where it can be handed to the middleware that decides.
	return buildHandler
}

// leaseHolder names this process in the leader lease: the pod name Kubernetes
// puts in HOSTNAME, or the OS hostname outside it.
func leaseHolder() string {
	if n := os.Getenv("HOSTNAME"); n != "" {
		return n
	}
	if n, err := os.Hostname(); err == nil {
		return n
	}
	return "loopzero-api"
}

// validateProductionEnv refuses to boot a production API without the secrets
// the deploy path cannot work without.
func validateProductionEnv(cfg config.Config, logger *slog.Logger) error {
	if !cfg.IsProduction() {
		return nil
	}
	return server.ValidateProductionEnv(logger, map[string]string{
		"DATABASE_URL":              os.Getenv("DATABASE_URL"),
		"LOOPZERO_ARGO_CALLBACK_SECRET": os.Getenv("LOOPZERO_ARGO_CALLBACK_SECRET"),
		"GITEA_TOKEN":               cfg.GiteaToken,
		"ARGOCD_TOKEN":              cfg.ArgocdToken,
	})
}

// logDatabaseSetupFailure names the two failures apart: "failed to connect"
// sent whoever forgot DATABASE_URL to look at the network.
func logDatabaseSetupFailure(err error, logger *slog.Logger) {
	if errors.Is(err, ErrDatabaseURLNotSet) {
		logger.Error("database not configured", "error", err)
		return
	}
	logger.Error("failed to connect to database", "error", err)
}

// withSecretResyncer makes a deploy repopulate the tenant's ExternalSecrets
// after the env ensurer, closing the set-secret-before-first-deploy race. It
// needs a value-returning secret store and the workload secret syncer used by
// POST /api/v1/secrets.
func withSecretResyncer(h *builds.BuildHandler, stores *StoreDeps, k8sDeps *K8sDeps, logger *slog.Logger) *builds.BuildHandler {
	if h == nil || stores == nil || k8sDeps == nil || k8sDeps.SecretK8sSync == nil {
		return h
	}
	logger.Info("secret resyncer wired: deploy repopulates ExternalSecrets after the env ensurer")
	return h.WithSecretResyncer(&platform.SecretResyncer{
		Store:  stores.PgSecretStore,
		K8s:    k8sDeps.SecretK8sSync,
		Logger: logger,
	})
}
