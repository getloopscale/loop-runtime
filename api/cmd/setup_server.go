// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"

	"github.com/getloopscale/loopzero/api/internal/builds/admission"
	"github.com/getloopscale/loopzero/api/internal/builds/buildsadapter"
	"github.com/getloopscale/loopzero/api/internal/builds/buildshttp"
	"github.com/getloopscale/loopzero/api/internal/environments/environmentsadapter"
	"github.com/getloopscale/loopzero/api/internal/environments/environmentshttp"
	"github.com/getloopscale/loopzero/api/internal/placement/placementadapter"
	"github.com/getloopscale/loopzero/api/internal/source"
	"github.com/getloopscale/loopzero/api/internal/teams/teamshttp"

	"github.com/getloopscale/loopzero/api/internal/release"

	previewsvc "github.com/getloopscale/loopzero/api/internal/preview"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/audit/audithttp"
	"github.com/getloopscale/loopzero/api/internal/billing/billinghttp"
	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/domains/domainshttp"
	"github.com/getloopscale/loopzero/api/internal/health/healthhttp"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/middleware/observability"
	policy "github.com/getloopscale/loopzero/api/internal/middleware/policy"
	ratelimit "github.com/getloopscale/loopzero/api/internal/middleware/ratelimit"
	"github.com/getloopscale/loopzero/api/internal/placement"
	"github.com/getloopscale/loopzero/api/internal/preview/previewadapter"
	previewroutes "github.com/getloopscale/loopzero/api/internal/preview/previewhttp"
	"github.com/getloopscale/loopzero/api/internal/server"
	"github.com/getloopscale/loopzero/api/internal/timeline/aggregator"
	"github.com/getloopscale/loopzero/api/internal/timeline/timelineadapter"
	"github.com/getloopscale/loopzero/pkg/enforcement"
	pkglogging "github.com/getloopscale/loopzero/pkg/logging"
)

// setupHealthServices creates deep health checkers for critical dependencies.
func setupHealthServices(cfg config.Config) []healthhttp.ServiceHealthChecker {
	var healthServices []healthhttp.ServiceHealthChecker
	if cfg.ArgocdURL != "" {
		healthServices = append(healthServices, healthhttp.NewHTTPServiceChecker(
			"argocd", cfg.ArgocdURL+"/api/v1/applications", cfg.ArgocdToken, nil))
	}
	if cfg.GiteaURL != "" {
		healthServices = append(healthServices, healthhttp.NewHTTPServiceChecker(
			"gitea", cfg.GiteaURL+"/api/v1/repos/search?limit=1", cfg.GiteaToken, nil))
	}
	return healthServices
}

// serverWiring is everything buildServerConfig composes. main.go passes nil
// for the database-backed groups (dbAdapter, dbDeps, stores) on the
// no-database boot, and for orchDeps, ghDeps, cellRouter and cellStore when
// their setup was skipped; k8sDeps is never nil (setupK8s failing exits).
type serverWiring struct {
	cfg       config.Config
	dbAdapter *db.PgxAdapter
	// crossTenant is the background pool (loopzero_worker), asserted able to
	// cross tenants at boot. Tenant provisioning runs on it; nil without a
	// database.
	crossTenant    *db.PgxAdapter
	dbDeps         *DatabaseDeps
	stores         *StoreDeps
	orchDeps       *OrchestratorDeps
	ghDeps         *GitHubDeps
	k8sDeps        *K8sDeps
	cellRouter     placement.Router
	cellStore      *store.PostgresCellStore
	domainHandler  *domainshttp.DomainHandler
	buildHandler   *buildshttp.BuildHandler
	healthServices []healthhttp.ServiceHealthChecker
	tokenValidator auth.TokenValidator
	rateLimiter    *ratelimit.RateLimiter
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
	logger         *slog.Logger
}

// buildServerConfig assembles the server.Config from the dependency groups.
// Each wire* step reads only the group it is named after and returns early
// when that group is nil, so no field is read above its own guard — the
// B-104 no-database boot panic was exactly that.
func buildServerConfig(w serverWiring) server.Config {
	sc := baseServerConfig(w)
	wireDatabaseDeps(&sc, w)
	wireCostEnforcement(&sc, w)
	wireStores(&sc, w)
	wireOrchestrator(&sc, w)
	wireGitHub(&sc, w)
	wireDatabaseHandlers(&sc, w)
	wireAdmin(&sc, w)
	wirePGPoolGate(&sc, w)
	wirePreviews(&sc, w)
	wirePlacement(&sc, w)
	return sc
}

func baseServerConfig(w serverWiring) server.Config {
	cfg := w.cfg
	return server.Config{
		Port:        cfg.Port,
		MetricsPort: cfg.MetricsPort,
		Logger:      w.logger,
		// ContextLogger enriches every handler's request-scoped logger with
		// correlation_id and, from the span OtelHTTP opened, trace_id.
		ContextLogger:  pkglogging.NewContextLogger(w.logger, observability.TraceIDs),
		HealthServices: w.healthServices,
		TokenValidator: w.tokenValidator,
		TenantCRMgr:    w.k8sDeps.TenantCRMgr,
		DomainHandler:  w.domainHandler,
		BuildHandler:   w.buildHandler,
		BuildAttestationHandler: buildshttp.NewBuildAttestationHandler(
			store.NewPostgresAttestationStore(w.dbAdapter), w.logger,
		),
		// Per-app SBOM endpoint over the release_app_attestations sidecar. With
		// no object uploader (no S3 in dev) it serves metadata without a
		// download_url.
		BuildSBOMHandler: buildshttp.NewBuildSBOMHandler(
			&buildsadapter.AppSBOMStore{Inner: store.NewAppAttestationStoreSqlc(w.dbAdapter)},
			toSBOMURLGenerator(w.stores),
			cfg.SourceBucket,
			w.logger,
		),
		EnvironmentHandler: newEnvironmentHandler(w),
		ShutdownCtx:        w.shutdownCtx,
		ShutdownCancel:     w.shutdownCancel,
		IsProduction:       cfg.IsProduction(),
		CORSAllowedOrigin:  cfg.CORSAllowedOrigin,
		Secrets:            server.SecretsConfig{K8sSync: w.k8sDeps.SecretK8sSync},
		Guards: server.RouteGuards{
			RateLimiter: w.rateLimiter,
			UpgradeURL:  cfg.BillingUpgradeURL,
		},
		Timeouts: server.HTTPTimeouts{
			ReadHeader: cfg.ReadHeaderTimeout,
			Read:       cfg.ReadTimeout,
			Idle:       cfg.IdleTimeout,
			Shutdown:   cfg.GracefulShutdownTime,
		},
		SSE: server.SSEConfig{
			HeartbeatInterval:    cfg.SSEHeartbeatInterval,
			PollInterval:         cfg.SSEPollInterval,
			SafetyTimeout:        cfg.SSESafetyTimeout,
			MaxConcurrentStreams: int64(cfg.SSEMaxConcurrentStreams),
		},
	}
}

// newEnvironmentHandler applies each collaborator only when the group that
// provides it exists. The project lookup is the cross-tenant gate (EC-1): the
// handler falls open without it, and it is absent only when there is no
// database — the same condition that makes the routes it guards unreachable.
func newEnvironmentHandler(w serverWiring) *environmentshttp.EnvironmentHandler {
	h := environmentshttp.NewEnvironmentHandler(w.k8sDeps.TenantCRMgr)
	if w.dbAdapter != nil {
		h = h.WithKPILookup(&environmentsadapter.EnvKPILookup{DB: w.dbAdapter})
	}
	if w.stores != nil {
		h = h.WithPreviewSlotLookup(&environmentsadapter.PreviewSlotLookup{Store: w.stores.PreviewSlotStore})
	}
	if w.dbDeps != nil {
		h = h.WithProjectLookup(&environmentsadapter.EnvProjectLookupAdapter{Store: w.dbDeps.ProjectStore})
	}
	return h
}

func wireDatabaseDeps(sc *server.Config, w serverWiring) {
	if w.dbDeps == nil {
		return
	}
	sc.MemberStore = w.dbDeps.MemberStore
	sc.ProjectStore = w.dbDeps.ProjectStore
	sc.DB = w.dbDeps.DBHealth
	sc.Guards.PlanLimiter = w.dbDeps.PlanLimiter
	if w.dbDeps.PlanLimiter != nil {
		sc.Guards.PlanUsage = store.NewPostgresPlanUsageStore(w.dbDeps.DBAdapter.Pool)
	}
	// The project store doubles as the secrets cross-tenant binding checker:
	// a cross-tenant attempt and a nonexistent project answer the same 404
	// (CWE-204).
	sc.Secrets.ProjectChecker = w.dbDeps.ProjectStore
}

// wireCostEnforcement installs the HTTP 402 cost-cap enforcer and hands the
// canonical plan-tier store to the handlers that resolve a tier. Without a
// working PlanLimiter the enforcer stays nil and the middleware passes
// through; before migration 000017 the cost store has no rows and the cap
// checker fails open (no usage counted).
func wireCostEnforcement(sc *server.Config, w serverWiring) {
	if w.dbDeps == nil || w.dbDeps.PlanLimits == nil || w.dbDeps.PlanLimiter == nil {
		return
	}
	// The same store PlanLimiter reads the plan through, so every tier gate
	// and the limiter agree on a tenant's plan.
	limitsStore := w.dbDeps.PlanLimits
	sc.Guards.CostEnforcer = policy.NewCostEnforcer(
		enforcement.NewCapChecker(store.NewSQLCostStore(w.dbAdapter)),
		store.NewPlanReader(limitsStore), w.logger)
	// Without the tier store the scale and status handlers fall back to
	// starter even for a team subscription (#123); the router wires the
	// project handler from sc.PlanTierStore.
	if w.orchDeps != nil && w.orchDeps.ScaleHandler != nil {
		w.orchDeps.ScaleHandler.SetPlanTierStore(limitsStore)
	}
	if w.orchDeps != nil && w.orchDeps.StatusHandler != nil {
		w.orchDeps.StatusHandler.SetPlanTierStore(limitsStore)
	}
	sc.PlanTierStore = limitsStore
}

func wireStores(sc *server.Config, w serverWiring) {
	stores := w.stores
	if stores == nil {
		return
	}
	sc.SourceStore = stores.SourceStore
	sc.ObjectUploader = stores.ObjUploader
	sc.SourceBucket = w.cfg.SourceBucket
	sc.EnvBlocklistMode = source.EnvBlocklistMode(w.cfg.EnvBlocklistMode)
	sc.EventStore = stores.EventStore
	// Secret writes take the same environment lock as deploys, so they
	// serialize on the same (tenant, project, env) key.
	sc.Secrets.EnvLocker = stores.Locker
	sc.BuildStoreReader = stores.BuildStoreReader
	// Deploy Timeline read store: omitted (route not registered) when either
	// the build lookup or the event store is missing.
	if blu, ok := stores.BuildStoreReader.(aggregator.BuildLookup); ok && stores.EventStore != nil {
		sc.DeployTimelineStore = aggregator.NewPostgresDeployTimelineStore(
			blu, timelineadapter.NewDeployTimelineSSEReader(stores.EventStore))
	}
	sc.Secrets.Store = stores.SecretStore
	sc.BootstrapSecret = w.cfg.BootstrapSecret
}

func wireOrchestrator(sc *server.Config, w serverWiring) {
	o := w.orchDeps
	if o == nil {
		return
	}
	sc.ArgoCallbackHandler = o.ArgoCallbackHandler
	sc.LogsHandler = o.LogsHandler
	sc.LogStreamHandler = o.LogStreamHandler
	sc.RollbackHandler = o.RollbackHandler
	sc.PromoteHandler = o.PromoteHandler
	sc.StatusHandler = o.StatusHandler
	sc.MetricsProxyHandler = o.MetricsProxyHandler
	sc.StatusSSEHandler = o.StatusSSEHandler
	sc.ScaleHandler = o.ScaleHandler
	sc.BuildListHandler = o.BuildListHandler
	sc.BuildStatsHandler = o.BuildStatsHandler
	sc.ProjectDeleter = o.ProjectDeleter
}

func wireGitHub(sc *server.Config, w serverWiring) {
	if w.ghDeps == nil {
		return
	}
	sc.GitHubWebhookHandler = w.ghDeps.WebhookHandler
	sc.RepositoryHandler = w.ghDeps.RepoHandler
	sc.IntegrationsGitHubHandler = w.ghDeps.IntegrationsHandler
}

// wireDatabaseHandlers wires the handlers that only need the request pool.
func wireDatabaseHandlers(sc *server.Config, w serverWiring) {
	d := w.dbAdapter
	if d == nil {
		return
	}
	sc.Guards.IdempotencyStore = &store.PostgresIdempotencyStore{DB: d}
	sc.Secrets.AuditLogger = &store.AuditLogger{DB: d}
	// GET /api/v1/billing: plan + usage, and the three cost metrics once the
	// cost store is reachable. The engine does NOT issue billing portal
	// sessions (ADR 2026-08-11): loopzero-cloud creates the Stripe customer and is
	// the only issuer, so no Stripe key lives on the CLI and deploy path.
	// The plan shown is the plan enforced: billing reads it through the same
	// store every plan gate uses (sc.PlanTierStore, set by
	// wireCostEnforcement when the database is up), not through its own query.
	billingHandler := billinghttp.NewBillingHandler(sc.PlanTierStore, store.NewSubscriptionStore(d))
	if w.dbDeps != nil && w.dbDeps.PlanLimiter != nil {
		billingHandler.WithCostSource(store.NewSQLCostStore(d))
	}
	sc.BillingHandler = billingHandler
	// One team store serves the team admin endpoints and the /whoami lookup.
	teamStore := &store.PostgresTeamStore{DB: d}
	sc.TeamHandler = teamshttp.NewTeamHandler(teamStore)
	sc.AuthUserLookup = teamStore
	sc.ActiveTeamStore = store.NewPostgresActiveTeamStore(d)
	sc.AuditHandler = audithttp.NewAuditHandler(&store.PostgresAuditStore{DB: d})
	// The soft-deletion cycle (DELETE /tenants/{id}, restore):
	// the router registers those routes only when this store is set (B-117).
	sc.TenantDeletionStore = store.NewTenantDeletionStoreSqlc(d)
	sc.AuditWriter = newAuditWriter(d, w.logger)
}

func newAuditWriter(d *db.PgxAdapter, logger *slog.Logger) func(tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) {
	return func(tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) {
		entry := store.AuditEntry{
			TenantID: tenantID, Action: action, Actor: actor,
			ResourceType: resourceType, ResourceID: resourceID,
			CorrelationID: correlationID, Metadata: metadata,
		}
		if err := store.WriteAuditLog(context.Background(), d, entry); err != nil {
			logger.Warn("audit write failed", "error", err)
		}
	}
}

// wireAdmin wires the admin surface: tenant eviction, the cell pin (the only
// path that moves a tenant; it also patches the Tenant CR so the operator
// re-emits the ArgoCD destination), and the read-only release inspector.
func wireAdmin(sc *server.Config, w serverWiring) {
	d := w.dbAdapter
	if d == nil {
		return
	}
	sc.Admin.Evictor = buildAdminEvictor(w.cfg, d)
	pinner := placement.NewCellPinner(&store.PostgresCellStore{DB: d})
	if mgmt := buildMgmtDynamicClient(w.cfg, w.logger); mgmt != nil {
		pinner = pinner.WithTenantCRPatcher(placementadapter.NewTenantCRCellPatcher(mgmt, "loopzero-system"))
	}
	sc.Admin.Pinner = pinner
	// The inspector's build store is nil on purpose: workflow_id degrades to
	// null, as when the workflow CR was reaped. It is read-only, so the no-op
	// event store never receives an emit.
	sc.Admin.ReleaseInspector = release.NewReleaseInspector(
		store.NewReleaseStoreSqlc(d, store.NoopEventStore{}),
		&store.PostgresAuditStore{DB: d},
		nil,
	)
}

// wirePGPoolGate installs the pool admission gate: the PGPoolBreaker, with
// hysteresis, decides — an instantaneous percentage per request oscillated
// around the cut-off line (B-054).
func wirePGPoolGate(sc *server.Config, w serverWiring) {
	if w.dbAdapter == nil {
		return
	}
	gate := admission.NewPGPoolBreaker(w.dbAdapter.Pool, admission.DefaultBreakerConfig(w.cfg.DBMaxConns), w.logger)
	gate.Start(w.shutdownCtx)
	go func() {
		<-w.shutdownCtx.Done()
		gate.Stop()
	}()
	sc.Guards.PGPoolGate = gate
	w.logger.Info("pg pool gate enabled",
		"high_water", 0.90, "low_water", 0.75, "max_conns", w.cfg.DBMaxConns)
}

// wirePreviews wires /api/v1/projects/{project}/previews. The service (CR +
// DB dual write, build submission) needs the orchestrator; without it, or if
// it fails to build, the handler serves read-only. The CR phase lookup shows
// the operator-observed status, and the project checker answers 404
// PROJECT_NOT_FOUND for a project that does not exist.
func wirePreviews(sc *server.Config, w serverWiring) {
	d := w.dbAdapter
	if d == nil {
		return
	}
	previewStore := store.NewPostgresPreviewStore(d, d)
	sc.PreviewHandler = previewroutes.
		NewPreviewHandler(previewStore, newPreviewService(w, previewStore), w.logger).
		WithPhaseLookup(w.k8sDeps.PreviewCRMgr).
		WithProjectChecker(&store.PostgresProjectStore{DB: d})
}

// newPreviewService returns the service as the route's interface and returns
// an untyped nil when there is none: a nil *PreviewService inside the
// interface would pass the handler's `service == nil` guard and be called.
func newPreviewService(w serverWiring, previewStore *store.PostgresPreviewStore) previewroutes.PreviewService {
	if w.orchDeps == nil || w.orchDeps.Orchestrator == nil || w.stores == nil {
		return nil
	}
	ps, err := previewsvc.NewPreviewService(previewsvc.PreviewServiceDeps{
		Store:               previewStore,
		Builds:              w.orchDeps.Orchestrator,
		BuildApps:           &previewadapter.PreviewAppsLookupAdapter{Builds: w.stores.BuildStore},
		BaseDomain:          w.cfg.BaseDomain,
		PreviewPublicDomain: w.cfg.PreviewPublicDomain,
		Logger:              w.logger,
		SlotStore:           w.stores.PreviewSlotStore,
		EnvTargets:          w.stores.EnvTargetStore,
	})
	if err != nil {
		w.logger.Error("preview service init failed (read-only mode)", "error", err)
		return nil
	}
	return ps
}

// wirePlacement wires tenant provisioning — the cell placement engine
// (round-robin under the advisory-lock serializer) and the bootstrap store
// that writes inside its transaction — and the cell discovery health check.
//
// Provisioning runs on the cross-tenant pool, not the request pool. Placement
// counts every cell's tenants, and bootstrap writes a tenant that does not
// exist yet, so no tenant scope can be set; under fail-closed RLS (migration
// 000062) the request role would count zero tenants per cell and be refused
// the tenant INSERT.
func wirePlacement(sc *server.Config, w serverWiring) {
	if w.crossTenant == nil {
		return
	}
	engine := placement.NewRoundRobinEngine(w.cellRouter, &store.PostgresCellStore{DB: w.crossTenant}, w.cfg.MaxTenantsPerCell)
	sc.CellPlacement = engine.WithSerializer(placement.NewAdvisoryLockSerializer(w.crossTenant.Pool))
	sc.BootstrapStore = &store.PostgresBootstrapStore{DB: w.crossTenant}
	if checker, ok := w.cellRouter.(healthhttp.CellDiscoveryChecker); ok {
		sc.CellDiscovery = checker
	}
}

// toSBOMURLGenerator returns a SBOMURLGenerator if the configured
// uploader supports presigning, else nil. When nil, the SBOM endpoint
// returns metadata-only (no download_url).
func toSBOMURLGenerator(stores *StoreDeps) buildshttp.SBOMURLGenerator {
	if stores == nil {
		return nil
	}
	if g, ok := stores.ObjUploader.(buildshttp.SBOMURLGenerator); ok {
		return g
	}
	return nil
}
