// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/loopzero/loopzero/api/internal/config"
	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/placement"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/server"
	"github.com/loopzero/loopzero/api/internal/services"
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
	sc := server.Config{
		Port:                         cfg.Port,
		MetricsPort:                  cfg.MetricsPort,
		Logger:                       logger,
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
		// AUDIT-10: single PostgresTeamStore instance serves both team admin
		// endpoints and /whoami username lookup.
		teamStore := &db.PostgresTeamStore{DB: dbAdapter}
		sc.TeamHandler = routes.NewTeamHandler(teamStore)
		sc.AuthUserLookup = teamStore
		sc.AuditHandler = routes.NewAuditHandler(&db.PostgresAuditStore{DB: dbAdapter})
		sc.AdminEvictor = buildAdminEvictor(cfg, dbAdapter)
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
