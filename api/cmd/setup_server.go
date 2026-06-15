// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/loopzero/loopzero/api/internal/config"
	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/placement"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/server"
	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/api/internal/sse"
	"github.com/loopzero/loopzero/pkg/enforcement"
	pkglogging "github.com/loopzero/loopzero/pkg/logging"
)

// setupHealthServices creates deep health checkers for critical dependencies.
func setupHealthServices(cfg config.Config) []routes.ServiceHealthChecker {
	var healthServices []routes.ServiceHealthChecker
	if cfg.ArgocdURL != "" {
		healthServices = append(healthServices, routes.NewHTTPServiceChecker(
			"argocd", cfg.ArgocdURL+"/api/v1/applications", cfg.ArgocdToken, nil))
	}
	if cfg.GiteaURL != "" {
		healthServices = append(healthServices, routes.NewHTTPServiceChecker(
			"gitea", cfg.GiteaURL+"/api/v1/repos/search?limit=1", cfg.GiteaToken, nil))
	}
	return healthServices
}

// buildServerConfig assembles the server.Config from all dependency groups.
func buildServerConfig(
	cfg config.Config,
	dbAdapter *db.PgxAdapter,
	dbDeps *DatabaseDeps,
	stores *StoreDeps,
	orchDeps *OrchestratorDeps,
	ghDeps *GitHubDeps,
	k8sDeps *K8sDeps,
	cellRouter placement.Router,
	cellStore *db.PostgresCellStore,
	domainHandler *routes.DomainHandler,
	buildHandler *routes.BuildHandler,
	healthServices []routes.ServiceHealthChecker,
	tokenValidator middleware.TokenValidator,
	rateLimiter *middleware.RateLimiter,
	shutdownCtx context.Context,
	shutdownCancel context.CancelFunc,
	logger *slog.Logger,
) server.Config {
	// Beta Logs Gate — ContextLogger wires correlation_id enrichment
	// into every handler's request-scoped logger. The OTel extractor is
	// nil here because trace_id enrichment is handled by the OtelHTTP
	// middleware separately; once an OTel-aware extractor is wired
	// (operators/cmd pattern), it can be passed in.
	contextLogger := pkglogging.NewContextLogger(logger, nil)

	sc := server.Config{
		Port:                         cfg.Port,
		MetricsPort:                  cfg.MetricsPort,
		Logger:                       logger,
		ContextLogger:                contextLogger,
		HealthServices:               healthServices,
		TokenValidator:               tokenValidator,
		TenantCRMgr:                  k8sDeps.TenantCRMgr,
		SecretK8sSync:                k8sDeps.SecretK8sSync,
		// Sprint A T1.1 (closes comp-001 CRITICAL — secret mutations were
		// running without advisory lock). Reuses the same locker injected
		// into build orchestrators, so secret writes serialize with deploys
		// on the same (tenant, project, env) key. Lock-domain parity EC-2.
		SecretEnvLocker:              stores.Locker,
		// T1.2 (plan 2026-06-10-j22-openbao-transit-eso-sync v1.2): inject
		// the existing *db.PostgresProjectStore as the cross-tenant binding
		// checker (EC-03 absorption — 404 byte-identical between cross-
		// tenant attempts + nonexistent projects; CWE-204 defense). The
		// PostgresProjectStore.GetByName signature already matches
		// routes.SecretProjectChecker — no adapter needed.
		SecretProjectChecker:         dbDeps.ProjectStore,
		DomainHandler:                domainHandler,
		BuildHandler:                 buildHandler,
		// CHG-2026-04-25-ATTESTATION-WIRE Phase 6: wire concrete
		// PostgresAttestationStore so /api/v1/builds/{id}/attestation
		// returns real data (was always 404 because the store was nil).
		BuildAttestationHandler: routes.NewBuildAttestationHandler(
			db.NewPostgresAttestationStore(dbAdapter), logger,
		),
		// Sprint 6 / T1.2: per-app SBOM endpoint. The store is the
		// release_app_attestations sidecar (T0.1). The presigner is
		// the existing S3 uploader (loopzero-static-assets bucket / sbom/
		// prefix). When stores.ObjUploader is nil (no S3 in dev), the
		// handler still serves metadata but omits download_url.
		BuildSBOMHandler: routes.NewBuildSBOMHandler(
			newAppSBOMStoreAdapter(dbAdapter),
			toSBOMURLGenerator(stores),
			cfg.SourceBucket,
			logger,
		),
		EnvironmentHandler: routes.NewEnvironmentHandler(k8sDeps.TenantCRMgr).
			WithKPILookup(&envKPILookupPgx{pool: dbAdapter.Pool}).
			WithPreviewSlotLookup(&previewSlotLookupAdapter{store: stores.PreviewSlotStore}).
			// M2 v1.2 T4.1 (LIVE-caught wiring gap 2026-06-08): the
			// EnvProjectLookup gate is REQUIRED in production so the
			// EC-1 cross-tenant project check actually fires. Without
			// this, the handler falls open (test-fixture mode) and a
			// user from tenant A could create an env under a project
			// owned by tenant B. The adapter scopes the lookup by
			// tenant_id via the canonical GetByName query.
			WithProjectLookup(&envProjectLookupAdapter{store: dbDeps.ProjectStore}),
		ShutdownCtx:                  shutdownCtx,
		ShutdownCancel:               shutdownCancel,
		RateLimiter:                  rateLimiter,
		ReadHeaderTimeout:            cfg.ReadHeaderTimeout,
		ReadTimeout:                  cfg.ReadTimeout,
		IdleTimeout:                  cfg.IdleTimeout,
		ShutdownTimeout:              cfg.GracefulShutdownTime,
		SSEHeartbeatInterval:         cfg.SSEHeartbeatInterval,
		SSEPollInterval:              cfg.SSEPollInterval,
		SSESafetyTimeout:             cfg.SSESafetyTimeout,
		SSEMaxConcurrentStreams:      int64(cfg.SSEMaxConcurrentStreams),
		IsProduction:                 cfg.IsProduction(),
		UpgradeURL:                   os.Getenv("BILLING_UPGRADE_URL"),
		PGCircuitBreakerThresholdPct: 90,
		CORSAllowedOrigin:           cfg.CORSAllowedOrigin,
	}

	if dbDeps != nil {
		sc.MemberStore = dbDeps.MemberStore
		sc.ProjectStore = dbDeps.ProjectStore
		sc.DB = dbDeps.DBHealth
		sc.PlanLimiter = dbDeps.PlanLimiter
	}

	// T1.9 production wiring: CostEnforcer for HTTP 402 on cost cap exceeded.
	// Built only when both DB pool and a working PlanLimiter are available —
	// otherwise CostEnforcer is nil and the route middleware is pass-through.
	// Migration 000017 must be applied for the CostStore reads to succeed;
	// pre-migration the store returns sql.ErrNoRows and the CapChecker
	// fails-open (no usage counted = allow).
	if dbDeps != nil && dbDeps.StdDB != nil && dbDeps.PlanLimiter != nil {
		// T2.2 #5 of sqlc-migration: cost_counters reads dispatch through
		// SqlcBridge (DBTX) — dbAdapter is the pgx-based DBTX, not the
		// legacy StdDB. StdDB is still retained for enforcement.LimitsStore
		// which has a wider surface and lives outside api/internal/db/.
		costStore := db.NewSQLCostStore(dbAdapter)
		costChecker := enforcement.NewCapChecker(costStore)
		// Reuse the same SQL `subscriptions.plan` lookup PlanLimiter uses,
		// via a fresh NewSQLLimitsStore. PlanLimiter does not expose its
		// internal store, so we construct one with the same DB handle —
		// the query is centralized in enforcement.sqlLimitsStore so this
		// is single-source-of-truth.
		planReader := db.NewPlanReader(enforcement.NewSQLLimitsStore(dbDeps.StdDB))
		sc.CostEnforcer = middleware.NewCostEnforcer(costChecker, planReader, logger)
	}

	if stores != nil {
		sc.SourceStore = stores.SourceStore
		sc.ObjectUploader = stores.ObjUploader
		sc.SourceBucket = cfg.SourceBucket
		sc.EventStore = stores.EventStore
		sc.BuildStoreReader = stores.BuildStoreReader
		sc.SecretStore = stores.SecretStore
		sc.BootstrapStore = stores.BootstrapStore
		sc.BootstrapSecret = cfg.BootstrapSecret
	}

	if orchDeps != nil {
		sc.ArgoCallbackHandler = orchDeps.ArgoCallbackHandler
		sc.LogsHandler = orchDeps.LogsHandler
		sc.LogStreamHandler = orchDeps.LogStreamHandler
		sc.RollbackHandler = orchDeps.RollbackHandler
		sc.PromoteHandler = orchDeps.PromoteHandler
		sc.StatusHandler = orchDeps.StatusHandler
		sc.MetricsProxyHandler = orchDeps.MetricsProxyHandler
		sc.ScaleHandler = orchDeps.ScaleHandler
		sc.BuildListHandler = orchDeps.BuildListHandler
		sc.ProjectDeleter = orchDeps.ProjectDeleter
	}

	if ghDeps != nil {
		sc.GitHubWebhookHandler = ghDeps.WebhookHandler
		sc.RepositoryHandler = ghDeps.RepoHandler
		sc.IntegrationsGitHubHandler = ghDeps.IntegrationsHandler
	}

	if dbAdapter != nil {
		sc.IdempotencyStore = &db.PostgresIdempotencyStore{DB: dbAdapter}
		// T1.6 of deep-review-remediation: wire BillingHandler so
		// GET /api/v1/billing returns plan + usage instead of 404.
		// T1.9 production: also wire the cost cap CostStore so the
		// /api/v1/billing response includes the 3 metrics (egress,
		// docr_pulls, build_minutes) with their current values + caps.
		// pre-migration-000017 the costSource returns no rows and
		// usage map stays empty (graceful degradation).
		billingHandler := routes.NewBillingHandler(billingPoolAdapter{pool: dbAdapter.Pool})
		if dbDeps != nil && dbDeps.StdDB != nil {
			// T2.2 #5 of sqlc-migration: dispatches via SqlcBridge (DBTX).
			billingHandler.WithCostSource(db.NewSQLCostStore(dbAdapter))
		}
		// T2.7 of maturity-3-62-to-4-0-roadmap: wire Stripe Customer
		// Portal SDK if STRIPE_SECRET_KEY env is set. Production sets
		// it via ESO from the prod Stripe secret. Local dev + tests
		// without the env keep the legacy placeholder URL — no SDK
		// call, no test pollution. Test-mode keys (sk_test_*) work
		// fine and return real session URLs against Stripe's test
		// dashboard, useful for staging environments.
		//
		// ADR-2026-05-27-T2.7-RAK conditional approval § "Engineering
		// deliverables" #1 — startup assertion (6th test condition per
		// CVP + GitOps): if LOOPZERO_ENV=production AND STRIPE_SECRET_KEY
		// does not start with sk_live_ prefix, panic with clear message
		// at boot. This is a defense-in-depth against silent test-mode
		// drift in production (revenue leak invisible from cluster).
		// Also rejects rk_live_/rk_test_ (Restricted Access Keys) at
		// the assertion level — RAK uses the same sk_live_ prefix per
		// Stripe docs, so the assertion correctly admits both full
		// keys + RAKs.
		if stripeKey := os.Getenv("STRIPE_SECRET_KEY"); stripeKey != "" {
			if err := validateStripeKeyForEnv(cfg.Env, stripeKey); err != nil {
				panic(err.Error())
			}
			returnURL := os.Getenv("BILLING_RETURN_URL")
			if returnURL == "" {
				returnURL = "https://app.loopzero.cloud/billing"
			}
			billingHandler.WithPortalSessionCreator(
				services.NewStripePortalSessionCreator(stripeKey),
				returnURL,
			)
		}
		sc.BillingHandler = billingHandler
		// AUDIT-10: single PostgresTeamStore instance serves both team admin
		// endpoints and /whoami username lookup.
		teamStore := &db.PostgresTeamStore{DB: dbAdapter}
		sc.TeamHandler = routes.NewTeamHandler(teamStore)
		sc.AuthUserLookup = teamStore
		// Plan T2.5 D8 full — persistent active-team selection backed by
		// users.active_tenant_id column (migration 000018).
		sc.ActiveTeamStore = db.NewPostgresActiveTeamStore(dbAdapter)
		sc.AuditHandler = routes.NewAuditHandler(&db.PostgresAuditStore{DB: dbAdapter})
		sc.AdminEvictor = buildAdminEvictor(cfg, dbAdapter)
		// CHG-2026-04-30-DELETE-SINGLE-CELL: wire the CellPinner so the
		// /api/v1/admin/tenants/pin endpoint registered in router.go can
		// actually serve requests. Previously the handler existed but had
		// no consumer, returning 404. loopzero admin tenants pin --cell <id>
		// is the only path to override automatic cell assignment for a
		// tenant.
		// CHG-2026-04-30-DELETE-SINGLE-CELL: pinner also patches Tenant CR
		// so operator reconcile re-emits ArgoCD destination with new cell.
		sc.AdminPinner = services.NewCellPinner(&db.PostgresCellStore{DB: dbAdapter}).
			WithKubeClient(buildMgmtDynamicClient(logger), "loopzero-system")
		// T4.4 of plan 2026-05-28-meet-001-engineering-closure: wire the
		// ReleaseInspector so GET /api/v1/admin/releases/{id}/inspect can
		// serve requests. buildStore is intentionally nil — the inspector
		// gracefully degrades workflow_id to null when nil (EC-14: same
		// behavior as when the workflow CR has been reaped). Operator
		// follow-up: once db.BuildStore gains GetWorkflowIDByBuildID,
		// wire the third argument to surface the workflow ID too.
		// T1.1c2c (plan 2026-05-29-sqlc-migration v1.3): sqlc-backed
		// wrapper satisfies db.ReleaseStore 1:1; ReleaseInspector
		// reads via the same interface.
		// T1.3.b v1.1: NewReleaseStoreSqlc requires non-nil eventStore.
		// ReleaseInspector is read-only path so AppendSideEffect emit
		// never fires; pass a no-op eventStore that returns 0/nil to
		// satisfy the non-nil contract without touching sse_events table.
		sc.ReleaseInspector = services.NewReleaseInspector(
			db.NewReleaseStoreSqlc(dbAdapter, db.NoopEventStore{}),
			&db.PostgresAuditStore{DB: dbAdapter},
			nil,
		)
		sc.AuditWriter = func(tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) {
			entry := db.AuditEntry{
				TenantID: tenantID, Action: action, Actor: actor,
				ResourceType: resourceType, ResourceID: resourceID,
				CorrelationID: correlationID, Metadata: metadata,
			}
			if err := db.WriteAuditLog(context.Background(), dbAdapter, entry); err != nil {
				logger.Warn("audit write failed", "error", err)
			}
		}
		sc.PGPoolStatsFunc = func() (int32, int32) {
			stat := dbAdapter.Pool.Stat()
			return stat.AcquiredConns(), stat.MaxConns()
		}
		// Sprint 5 / T2.3 + T1.1 — Preview environments handler + service.
		// The handler exposes /api/v1/projects/{project}/previews; the
		// service drives the dual-write (CR + DB) per ADR D7. When the
		// BuildOrchestrator is available (always, post-Sprint-2), the
		// service can submit builds for previews (best-effort).
		previewStore := db.NewPostgresPreviewStore(dbAdapter, dbAdapter)
		var previewService *services.PreviewService
		if orchDeps != nil && orchDeps.Orchestrator != nil {
			ps, perr := services.NewPreviewService(services.PreviewServiceDeps{
				Store:               previewStore,
				Builds:              orchDeps.Orchestrator,
				BuildApps:           &previewAppsLookupAdapter{builds: stores.BuildStore},
				BaseDomain:          cfg.BaseDomain,
				PreviewPublicDomain: cfg.PreviewPublicDomain,
				Logger:              logger,
				// 2026-06-07 D2/D3/D6: single-slot SOT + env_target preview kind.
				SlotStore:  stores.PreviewSlotStore,
				EnvTargets: stores.EnvTargetStore,
			})
			if perr != nil {
				logger.Error("preview service init failed (read-only mode)", "error", perr)
			} else {
				previewService = ps
			}
		}
		// LIVE catch 2026-06-07 J-23 bug #5: enrich /previews with CR
		// phase so dashboard reflects operator-observed status. Manager
		// is canonical (k8sDeps.PreviewCRMgr), not a per-handler
		// adapter — same dynamic client + namespace const as Tenant CRs.
		sc.PreviewHandler = routes.
			NewPreviewHandler(previewStore, previewService, logger).
			WithPhaseLookup(k8sDeps.PreviewCRMgr)
		sc.CellPlacement = func() routes.CellPlacementEngine {
			engine := placement.NewRoundRobinEngine(cellRouter, cellStore, cfg.MaxTenantsPerCell)
			return engine.WithSerializer(placement.NewAdvisoryLockSerializer(dbAdapter.Pool))
		}()
		if checker, ok := cellRouter.(routes.CellDiscoveryChecker); ok {
			sc.CellDiscovery = checker
		}
	}

	return sc
}

// appSBOMStoreAdapter bridges the per-app attestation store
// (db.PostgresAppAttestationStore) into routes.AppSBOMStore by projecting
// AppAttestationInfo onto the routes-shape AppSBOMInfo. The adapter
// keeps the routes layer decoupled from db types.
//
// Sprint 6 / T1.2.
// T1.3b (plan 2026-05-29-sqlc-migration v1.3): adapter inner type
// widened from concrete *PostgresAppAttestationStore to
// db.AppAttestationStore interface so AppAttestationStoreSqlc drops in
// without churn.
type appSBOMStoreAdapter struct {
	inner db.AppAttestationStore
}

func newAppSBOMStoreAdapter(d *db.PgxAdapter) *appSBOMStoreAdapter {
	return &appSBOMStoreAdapter{inner: db.NewAppAttestationStoreSqlc(d)}
}

func (a *appSBOMStoreAdapter) GetBuildAppSBOMs(
	ctx context.Context, tenantID, buildID string,
) ([]routes.AppSBOMInfo, error) {
	infos, err := a.inner.GetBuildAppAttestations(ctx, tenantID, buildID)
	if err != nil {
		return nil, err
	}
	out := make([]routes.AppSBOMInfo, 0, len(infos))
	for _, in := range infos {
		out = append(out, routes.AppSBOMInfo{
			AppName:            in.AppName,
			SBOMS3Key:          in.SBOMS3Key,
			SBOMFormat:         in.SBOMFormat,
			SBOMComponentCount: in.SBOMComponentCount,
			SBOMGeneratedAt:    in.SBOMGeneratedAt,
			Signed:             in.Signed,
		})
	}
	return out, nil
}

// toSBOMURLGenerator returns a SBOMURLGenerator if the configured
// uploader supports presigning, else nil. When nil, the SBOM endpoint
// returns metadata-only (no download_url).
func toSBOMURLGenerator(stores *StoreDeps) routes.SBOMURLGenerator {
	if stores == nil {
		return nil
	}
	if g, ok := stores.ObjUploader.(routes.SBOMURLGenerator); ok {
		return g
	}
	return nil
}

// buildMgmtDynamicClient constructs a dynamic.Interface for the mgmt
// cluster. CHG-2026-04-30-DELETE-SINGLE-CELL: required by CellPinner to
// patch tenant.spec.cellID after a successful DB update. Prefers
// in-cluster config so loopzero-api SA's RBAC applies (loopzero.io/tenants
// resource permission granted via clusterrolebinding/loopzero-api).
//
// Returns nil on failure (logged) — pinner falls back to DB-only mode
// which logs a manual `kubectl patch` hint when the CR patch is skipped.
func buildMgmtDynamicClient(logger *slog.Logger) dynamic.Interface {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		logger.Warn("cell pinner: in-cluster config unavailable, CR patches disabled",
			"error", err)
		return nil
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		logger.Warn("cell pinner: dynamic.NewForConfig failed, CR patches disabled",
			"error", err)
		return nil
	}
	return dyn
}

// billingPoolAdapter bridges *pgxpool.Pool to routes.BillingPool. The
// routes package defines its own narrow Row/Pool interfaces to avoid
// the routes ↔ db import cycle (find_arch_001). pgx.Row satisfies the
// routes.BillingRow contract (single Scan method) — this adapter just
// re-types the QueryRow signature.
type billingPoolAdapter struct {
	pool interface {
		QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	}
}

func (a billingPoolAdapter) QueryRow(ctx context.Context, sql string, args ...any) routes.BillingRow {
	return a.pool.QueryRow(ctx, sql, args...)
}

// validateStripeKeyForEnv is the 6th test condition of T2.7 conditional
// approval (per CVP + GitOps in MEET-2026-05-27 + ADR-2026-05-27-T2.7-RAK).
//
// Production environments require Stripe LIVE keys; test-mode keys
// (sk_test_*) MUST NOT be used in production because they are non-functional
// against real customer accounts — billing would silently fail, but the
// API would still claim to be wired (test mode keys return real-looking
// URLs that point at Stripe's test dashboard, NOT customer portals).
// This drift would be invisible from cluster monitoring (no 4xx/5xx from
// Stripe API — keys work; they just work against the test world).
//
// The validation runs at boot time so misconfiguration fails-fast at
// service startup rather than at first customer interaction. Operator
// must either (a) provide a sk_live_* key via ESO Secret, or (b) unset
// STRIPE_SECRET_KEY to fall back to the legacy placeholder URL (and
// accept that Portal endpoint will return a non-functional link).
//
// Stripe Restricted Access Keys (RAKs) use the same sk_live_ prefix per
// Stripe documentation, so this assertion correctly admits both full
// keys + RAKs in production.
//
// Returns nil if validation passes; non-nil error if production env
// has a non-live key prefix.
func validateStripeKeyForEnv(env, stripeKey string) error {
	// Only enforce in production. Development/staging may use test keys.
	if !isProductionEnv(env) {
		return nil
	}
	if strings.HasPrefix(stripeKey, "sk_live_") {
		return nil
	}
	// Truncate prefix for safe error messages (never log full key).
	prefix := stripeKey
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	return fmt.Errorf(
		"BOOT REFUSED: LOOPZERO_ENV=%q requires STRIPE_SECRET_KEY prefix sk_live_*, "+
			"got prefix %q. This is a defense-in-depth check against silent test-mode "+
			"drift in production (revenue leak invisible from cluster). Per "+
			"ADR-2026-05-27-T2.7-RAK. Rotate the ESO Secret to a sk_live_* key OR "+
			"unset STRIPE_SECRET_KEY to fall back to the legacy placeholder URL.",
		env, prefix,
	)
}

// isProductionEnv matches the convention from config.IsProduction().
// Mirrored locally (not imported) so this helper is testable without
// constructing a full Config struct.
func isProductionEnv(env string) bool {
	return strings.EqualFold(env, "production")
}

// =============================================================================
// startArgoStepWatcher — Plan J-17 T1.3 production wiring
// =============================================================================
//
// Polls Argo Workflows on the build cluster every 2s, extracts Pod node
// phase transitions, and emits build.step.{started,succeeded,failed}
// events to the SSE journal (sse_events table). Without this goroutine
// the dashboard's BuildTimelineLive shows zero step events even though
// the workflow is running — the LIVE bug caught 2026-06-06 J-17 dogfood
// (image loopzero-api:j17-iter1 missed this wiring on first roll-out).
//
// Failure modes are non-fatal: the watcher logs + skips a tick. EC-1
// surfaces 401/403 to the dedicated counter so Grafana alerts fire
// before silent token expiry breaks visibility.
//
// Lifecycle: started before the HTTP server, cancelled at shutdown.
func startArgoStepWatcher(ctx context.Context, cfg config.Config, eventStore sse.EventStore, logger *slog.Logger) {
	if eventStore == nil {
		logger.Warn("argo step watcher: event store nil, skipping")
		return
	}

	// Build a dynamic client connected to the build cluster, mirroring the
	// pattern in argo.NewWorkflowSubmitter. clientcmd handles in-cluster
	// SA token + ExplicitPath kubeconfig for local dev.
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.BuildKubeconfig}
	overrides := &clientcmd.ConfigOverrides{}
	if cfg.BuildKubeContext != "" {
		overrides.CurrentContext = cfg.BuildKubeContext
	}
	restCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		logger.Warn("argo step watcher: build cluster config failed, watcher disabled",
			"error", err)
		return
	}
	dyn, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		logger.Warn("argo step watcher: dynamic.NewForConfig failed, watcher disabled",
			"error", err)
		return
	}

	// Empty namespace = all namespaces. Build pipelines run under per-tenant
	// namespaces (tn-<tenantID>) so listing everything is the cheapest path.
	source := services.NewK8sWorkflowNodeSource(dyn, "", 2*time.Second, logger)
	// terminalIDs is nil — first-tick startup may emit duplicate step events
	// for already-running workflows; the watcher's internal lastPhase dedup
	// catches duplicates from tick 2 onwards. A future enhancement can wire
	// a BuildStore-backed TerminalBuildIDStore for cleaner cold start.
	watcher := services.NewArgoStepWatcher(source, eventStore, nil, logger)
	source.WithWatchErrorHandler(watcher.ReportWatchError)

	go func() {
		if err := watcher.Start(ctx); err != nil && ctx.Err() == nil {
			logger.Warn("argo step watcher: stopped unexpectedly", "error", err)
		}
	}()
}

// envKPILookupPgx implements routes.EnvKPILookup using direct pgx queries.
// J-10 v1.1 LIVE catch 2026-06-07 — populates the dashboard
// EnvironmentKPIRow with last_deploy timestamp + git commit SHA.
//
// DESIGN: one round-trip per List call (1 SQL). DISTINCT ON (environment)
// returns the most recent ACTIVE release per env across all projects of
// the tenant.
type envKPILookupPgx struct {
	pool interface {
		Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	}
}

func (l *envKPILookupPgx) LatestActivePerEnv(ctx context.Context, tenantID string) (map[string]routes.EnvKPI, error) {
	const q = `
		SELECT DISTINCT ON (environment)
			environment, git_commit, created_at
		FROM releases
		WHERE tenant_id = $1 AND status = 'ACTIVE'
		ORDER BY environment, created_at DESC
	`
	rows, err := l.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("envKPILookupPgx: %w", err)
	}
	defer rows.Close()
	result := make(map[string]routes.EnvKPI)
	for rows.Next() {
		var env, sha string
		var createdAt time.Time
		if scanErr := rows.Scan(&env, &sha, &createdAt); scanErr != nil {
			return nil, fmt.Errorf("envKPILookupPgx scan: %w", scanErr)
		}
		result[env] = routes.EnvKPI{
			LastDeploy:    createdAt.UTC().Format(time.RFC3339),
			LastDeploySha: sha,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("envKPILookupPgx rows: %w", err)
	}
	return result, nil
}

// previewSlotLookupAdapter implements routes.PreviewSlotLookup using
// the db.PreviewSlotStore.
//
// 2026-06-07 per-project model (migration 000028): ListPreviewSlots
// returns ALL slots for the tenant — one per project. Empty slice
// when the tenant has no previews; never nil.
type previewSlotLookupAdapter struct {
	store *db.PostgresPreviewSlotStore
}

func (a *previewSlotLookupAdapter) ListPreviewSlots(ctx context.Context, tenantID string) ([]routes.PreviewSlotInfo, error) {
	slots, err := a.store.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]routes.PreviewSlotInfo, 0, len(slots))
	for _, slot := range slots {
		out = append(out, routes.PreviewSlotInfo{
			ProjectID:       slot.ProjectID,
			ActivePRNumber:  slot.ActivePRNumber,
			ActiveBranch:    slot.ActiveBranch,
			ActiveReleaseID: slot.ActiveReleaseID,
			URL:             slot.URL,
			UpdatedAt:       slot.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}
