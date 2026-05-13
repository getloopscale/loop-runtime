// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"github.com/loopzero/loopzero/api/internal/config"
	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/placement"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/server"
	"github.com/loopzero/loopzero/api/internal/services"
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
		EnvironmentHandler:           routes.NewEnvironmentHandler(k8sDeps.TenantCRMgr),
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
		costStore := db.NewSQLCostStore(dbDeps.StdDB)
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
		sc.ScaleHandler = orchDeps.ScaleHandler
		sc.BuildListHandler = orchDeps.BuildListHandler
		sc.ProjectDeleter = orchDeps.ProjectDeleter
	}

	if ghDeps != nil {
		sc.GitHubWebhookHandler = ghDeps.WebhookHandler
		sc.RepositoryHandler = ghDeps.RepoHandler
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
			billingHandler.WithCostSource(db.NewSQLCostStore(dbDeps.StdDB))
		}
		sc.BillingHandler = billingHandler
		// AUDIT-10: single PostgresTeamStore instance serves both team admin
		// endpoints and /whoami username lookup.
		teamStore := &db.PostgresTeamStore{DB: dbAdapter}
		sc.TeamHandler = routes.NewTeamHandler(teamStore)
		sc.AuthUserLookup = teamStore
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
				Store:      previewStore,
				Builds:     orchDeps.Orchestrator,
				BaseDomain: cfg.BaseDomain,
				Logger:     logger,
			})
			if perr != nil {
				logger.Error("preview service init failed (read-only mode)", "error", perr)
			} else {
				previewService = ps
			}
		}
		sc.PreviewHandler = routes.NewPreviewHandler(previewStore, previewService, logger)
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
type appSBOMStoreAdapter struct {
	inner *db.PostgresAppAttestationStore
}

func newAppSBOMStoreAdapter(d *db.PgxAdapter) *appSBOMStoreAdapter {
	return &appSBOMStoreAdapter{inner: db.NewPostgresAppAttestationStore(d)}
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
