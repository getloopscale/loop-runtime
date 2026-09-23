// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package main is the entrypoint for the LoopZero API server.
package main

import (
	"context"
	"errors"
	"flag"
	"github.com/getloopscale/loopzero/api/internal/leader"
	"log/slog"
	"net/http"
	"net/http/pprof"
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
	"github.com/getloopscale/loopzero/api/internal/routes/secrets"
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
			// Same pattern as operators/internal/operatorboot/pprof.go iter 11.
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
		// Two different failures reach here, and naming them apart is the whole
		// diagnostic the operator gets on the first `kubectl logs`. "failed to
		// connect" sent whoever forgot the variable to look at the network.
		if errors.Is(err, ErrDatabaseURLNotSet) {
			logger.Error("database not configured", "error", err)
		} else {
			logger.Error("failed to connect to database", "error", err)
		}
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
	trustedProxyCIDRs := clientip.ParseTrustedProxyCIDRs(os.Getenv("LOOPZERO_TRUSTED_PROXY_CIDRS"))
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
		// Os loops de background NÃO usam o pool das requisições. Ele é
		// deliberadamente incapaz de ver outro tenant (AssertRequestPoolIsolated
		// recusa subir se conseguir), e varredura entre tenants é exatamente o
		// trabalho deles — sobre aquele pool, cada varredura leria e apagaria
		// NADA, sem erro e sem log. Ver setup_background_database.go.
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
		if ps, ok := dbDeps.ProjectStore.(*store.PostgresProjectStore); ok {
			wireProjectCap(ps, projectCap)
		}
		workflows, err = setupWorkflows(cfg, httpTransport, logger)
		if err != nil {
			logger.Error("failed to setup workflows", "error", err)
			os.Exit(1)
		}
		orchDeps = setupOrchestrator(dbAdapter, stores, workflows, cfg, httpTransport, logger, projectCap)
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

		dh, domainProvisioner, domainHealthChecker, cfZoneClient := setupDomains(stores, workflows, cfg, logger)
		domainHandler = dh

		// The election runs in a goroutine so the HTTP server starts at once;
		// standby replicas serve traffic while waiting (MEET-ASYNC-AMENDMENT-
		// 2026-05-12-003 Option A). The lease is the fenced generation lease
		// the controller binary uses, under the same name, so the two binaries
		// never run the loops together. With LOOPZERO_DISABLE_LEGACY_LOOPS=true the
		// API does not compete: the controller owns the loops.
		if cfg.DisableLegacyLoops {
			logger.Info("LOOPZERO_DISABLE_LEGACY_LOOPS=true — loops will not run here; controller binary owns them")
			metrics.LeaderStatus.Set(0)
		} else {
			leaderCfg := leader.DefaultConfig()
			ticker := time.NewTicker(leaderCfg.HeartbeatInterval)
			election := &loopsElection{
				acquirer: leader.NewAcquirer(bgAdapter, leaderCfg),
				name:     leader.BackgroundLoopsLeaseName,
				cellID:   db.CellDefault,
				holder:   leaseHolder(),
				tick:     ticker.C,
				logger:   logger,
			}
			bgWg.Add(1)
			go func() {
				defer bgWg.Done()
				defer ticker.Stop()
				election.run(shutdownCtx, func(loopsCtx context.Context) {
					startLeaderLoops(loopsCtx, &bgWg,
						bgAdapter, bgStores, workflows, orchDeps, k8sDeps, cellRouter,
						domainProvisioner, domainHealthChecker, cfZoneClient, cfg, logger)
				})
			}()
		}
	}
	defer waitForBackgroundGoroutines(&bgWg, logger)

	buildHandler := setupBuildHandler(dbAdapter, orchDeps, cellStore, cfg, shutdownCtx, logger)

	// T4.1 of plan 2026-06-09-m2-operator-canonical-fix v1.4 (ADR D4):
	// hoisted outside the EnvironmentEnsurer-wiring block below so the
	// same instance is also assigned to srvCfg.ProjectEnvEnsurer at line
	// ~325. POST /api/v1/projects then auto-creates a `production` env
	// without a second wire-up. nil when k8sDeps is unavailable (test or
	// degraded boot) → server.Router falls back to pre-T4.1 behavior.
	var projectEnvEnsurer promote.EnvironmentEnsurer

	// Plan T2.2 D5 — wire EnvironmentEnsurer so loopzero deploy --env <novo>
	// auto-creates the Environment CR + namespace + ArgoCD App on first
	// use. Closes F-S1-02 (dogfood 2026-05-15). Wiring requires the
	// TenantCRManager from k8sDeps; both ensurer + BuildHandler are
	// composed here in main so we keep the dependency tree explicit.
	if buildHandler != nil && k8sDeps != nil && k8sDeps.TenantCRMgr != nil {
		// m37 plan canonical fix (2026-06-25): the closure is now per-(project,
		// env) tuple. The pre-m37 closure matched by env name alone, which
		// silently returned exists=true when a DIFFERENT project on the same
		// tenant already used the env name → AddEnvironment was skipped →
		// Tenant CR spec.environments[] never gained the new tuple →
		// AppProject destinations never extended → ArgoCD sync failed:
		// "namespace not permitted in project". Closes #37 cenário 4.
		//
		// History (preserved for traceability):
		//   - M2 v1.2 T3.3 (2026-06-07): AddEnvironment gained the project arg.
		//   - 2026-06-08: the previous attempt used GetEnvironmentDetail
		//     (which is project-aware) but the project="" production-legacy
		//     pathway broke deploy-hang. The 2026-06-08 fix walked
		//     ListEnvironments and matched on name only — solving the hang
		//     but reintroducing the m37 silent no-op gap.
		//   - 2026-06-25 (this plan): tuple-aware match closes both gaps:
		//     legacy (project="") matches ONLY entries with empty Project;
		//     post-M2 tuples match by exact (Name, Project) pair.
		lister, adder := platform.NewEnvManagerAdapter(
			k8sDeps.TenantCRMgr.AddEnvironment,
			func(ctx context.Context, tenantID, project, envName string) (any, error) {
				envs, err := k8sDeps.TenantCRMgr.ListEnvironments(ctx, tenantID)
				if err != nil {
					return nil, err
				}
				for i := range envs {
					if envs[i].Name == envName && envs[i].Project == project {
						return &envs[i], nil
					}
				}
				return nil, nil
			},
		)
		// Sprint A trio-fix 2026-06-01: ReadinessChecker queries Environment
		// CR.status.phase=="Active" before EnsureEnvironment returns.
		//
		// 2026-06-02 H1 validation: TEMPORARILY DISABLED via LOOPZERO_ENV_READINESS_CHECKER
		// gate (default false). Hypothesis: the readiness poll adds 2-30s per
		// deploy × 11 journeys = +5min total, esgotando o budget 1h30m do
		// suite timeout. Pre-Sprint-A baseline (commit 098b6846, 2026-05-31)
		// passou 112/0/20 em 1h11m SEM esse gate. Re-enable após confirmar
		// hypothesis OR re-tune PollInterval/WaitTimeout para minimizar custo.
		ensurer := &platform.EnvironmentEnsurer{
			Lister: lister,
			Adder:  adder,
			Logger: logger,
		}
		if os.Getenv("LOOPZERO_ENV_READINESS_CHECKER") == "true" {
			if mgmtDynamic := buildMgmtDynamicClient(logger); mgmtDynamic != nil {
				ensurer.ReadinessChecker = adapters.NewEnvironmentCRReader(mgmtDynamic, "loopzero-system")
				logger.Info("env readiness checker wired — EnsureEnvironment will poll Environment CR.status.phase before returning (Sprint A trio-fix 2026-06-01)")
			}
		} else {
			logger.Info("env readiness checker SKIPPED — LOOPZERO_ENV_READINESS_CHECKER=false (H1 validation 2026-06-02)")
		}
		buildHandler = buildHandler.WithEnvironmentEnsurer(ensurer)
		// T4.1 of plan 2026-06-09-m2-operator-canonical-fix v1.4: capture
		// the same instance so the project Create handler can also use it
		// for ADR D4 production env auto-create (see srvCfg assignment below).
		projectEnvEnsurer = ensurer
		// Red Team loopzero-cloud#158: promote path was missing the m37/#37
		// EnsureEnvironment retrofit that builds/projects already had.
		// Without it, `loopzero promote --from preview --to production` for a
		// project whose production env had NEVER been created reported
		// success but Tenant CR spec.environments[] stayed short of the
		// (project, "production") tuple → AppProject destinations never
		// extended → ArgoCD sync failed → release SYNC_FAILED → the printed
		// URL 404s. Wire the SAME ensurer instance so all three code paths
		// (builds / project-create / promote) share one canonical ensurer.
		if orchDeps != nil && orchDeps.PromoteHandler != nil {
			orchDeps.PromoteHandler = orchDeps.PromoteHandler.WithEnvironmentEnsurer(ensurer)
			logger.Info("env ensurer wired to promote handler — promote --to <novo> will auto-create target env (#158)")
		}
		logger.Info("env ensurer wired — deploy --env <novo> will auto-create environments (T2.2 D5); also wired to project Create handler for ADR D4 production env auto-create (T4.1)")
	}

	// V9 L20: SecretResyncer wired post-envEnsurer in builds.go. Closes
	// the set-secret-BEFORE-first-deploy race (audit
	// 2026-05-30-V9-L19-99percent). Requires both a value-returning
	// secret store (PostgresSecretStore.ListForResync) and a workload
	// K8s creator (the same SecretK8sSync used by POST /api/v1/secrets).
	if buildHandler != nil && stores != nil && k8sDeps != nil && k8sDeps.SecretK8sSync != nil {
		resyncer := &platform.SecretResyncer{
			Store:  stores.PgSecretStore,
			K8s:    k8sDeps.SecretK8sSync,
			Logger: logger,
		}
		buildHandler = buildHandler.WithSecretResyncer(resyncer)
		logger.Info("secret resyncer wired — deploy will repopulate ExternalSecrets post envEnsurer (V9 L20)")
	}

	healthServices := setupHealthServices(cfg)

	rateLimiter := ratelimit.NewRateLimiter(60, 1*time.Minute)

	// T2.4 of deep-review-remediation-plan + ADR D4 — pre-auth IP rate limit.
	// 120/min/IP — enough headroom for legitimate batch CLIs, stings on
	// brute-force.
	ipRateLimiter := ratelimit.NewIPRateLimiter(ratelimit.IPRateLimitConfig{
		Limit:             120,
		Window:            1 * time.Minute,
		TrustedProxyCIDRs: trustedProxyCIDRs,
	})

	srvCfg := buildServerConfig(cfg, dbAdapter, dbDeps, stores, orchDeps, ghDeps,
		k8sDeps, cellRouter, cellStore, domainHandler, buildHandler,
		healthServices, authDeps.TokenValidator, rateLimiter,
		shutdownCtx, shutdownCancel, logger)
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

	// Layer 2 ESO sync chain (plan 2026-06-10-j22-openbao-transit-eso-sync
	// v1.2 D3): wire Secrets.FetchHandler for the broker endpoint
	// /internal/v1/secret-fetch/{tenant_id}/{secret_name}. ESO running in
	// mgmt cluster (Wave -1 of core.helmfile) calls this endpoint via the
	// webhook ClusterSecretStore to resolve user secrets — loopzero-api loads
	// the vault:v1: ciphertext from PG + decrypts via OpenBao transit +
	// returns plaintext_b64. Wires nil-safely: if any dep is missing the
	// handler stays nil and router skips route registration. LIVE-caught
	// gap closure 2026-06-10 J-22 walkthrough.
	if transit != nil && dbAdapter != nil && cellStore != nil {
		// Construct a fresh AuditLogger pointing at the same pool — the
		// orchestrator uses the same shape inline (setup_orchestrator.go:101)
		// rather than exposing it as a field. AuditLogger is a thin DB
		// wrapper, so this is single-source-of-truth even when constructed
		// twice (both writes go to the same audit_log table via WriteAuditLog).
		auditLogger := &store.AuditLogger{DB: dbAdapter}
		fetchDeps := setupSecretFetchDeps(stores.PgSecretStore, cellStore, auditLogger)
		brokerToken := os.Getenv("LOOPZERO_BROKER_TOKEN")
		if fetchDeps != nil && brokerToken != "" {
			rawHandler := secrets.NewSecretFetchHandler(*fetchDeps)
			srvCfg.Secrets.FetchHandler = brokerBearerMiddleware(brokerToken, rawHandler)
		} else if fetchDeps != nil {
			logger.Warn("secret-fetch broker disabled: LOOPZERO_BROKER_TOKEN not set", "component", "broker")
		}
	}
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
	querier := &buildStoreAdmissionQuerier{store: store.NewBuildStoreSqlc(dbAdapter)}
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
	// B-054: o breaker do pool Postgres era construido AQUI e nunca consultado — ele protege a
	// API inteira, nao o handler de builds, e construi-lo neste ponto o deixava preso numa
	// funcao que devolve so' o handler. Passou para `setup_server.go`, onde a config do servidor
	// e' montada e onde ele pode ser entregue ao middleware que decide.
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
