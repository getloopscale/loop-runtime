// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/getloopscale/loopzero/api/internal/middleware/observability"

	"github.com/getloopscale/loopzero/api/internal/buildplane/admission"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/routes/builds"
	"github.com/getloopscale/loopzero/api/internal/routes/environments"
	"github.com/getloopscale/loopzero/api/internal/routes/teams"

	"github.com/getloopscale/loopzero/api/internal/services/release"

	previewsvc "github.com/getloopscale/loopzero/api/internal/services/preview"

	// Aliased because this file ALSO imports `api/internal/placement`, and both
	// packages are named `placement`. The disambiguation lives here, in the only
	// file that needs it, instead of being baked into the package name for
	// everyone (B-057). Measured: of the 21 files that import one of the two,
	// only this one imports both.
	placementsvc "github.com/getloopscale/loopzero/api/internal/services/placement"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/adapters"
	"github.com/getloopscale/loopzero/api/internal/aggregator"
	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	policy "github.com/getloopscale/loopzero/api/internal/middleware/policy"
	ratelimit "github.com/getloopscale/loopzero/api/internal/middleware/ratelimit"
	"github.com/getloopscale/loopzero/api/internal/placement"
	"github.com/getloopscale/loopzero/api/internal/routes/audit"
	"github.com/getloopscale/loopzero/api/internal/routes/billing"
	"github.com/getloopscale/loopzero/api/internal/routes/domains"
	"github.com/getloopscale/loopzero/api/internal/routes/health"
	previewroutes "github.com/getloopscale/loopzero/api/internal/routes/preview"
	"github.com/getloopscale/loopzero/api/internal/server"
	"github.com/getloopscale/loopzero/pkg/enforcement"
	pkglogging "github.com/getloopscale/loopzero/pkg/logging"
)

// setupHealthServices creates deep health checkers for critical dependencies.
func setupHealthServices(cfg config.Config) []health.ServiceHealthChecker {
	var healthServices []health.ServiceHealthChecker
	if cfg.ArgocdURL != "" {
		healthServices = append(healthServices, health.NewHTTPServiceChecker(
			"argocd", cfg.ArgocdURL+"/api/v1/applications", cfg.ArgocdToken, nil))
	}
	if cfg.GiteaURL != "" {
		healthServices = append(healthServices, health.NewHTTPServiceChecker(
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
	cellStore *store.PostgresCellStore,
	domainHandler *domains.DomainHandler,
	buildHandler *builds.BuildHandler,
	healthServices []health.ServiceHealthChecker,
	tokenValidator auth.TokenValidator,
	rateLimiter *ratelimit.RateLimiter,
	shutdownCtx context.Context,
	shutdownCancel context.CancelFunc,
	logger *slog.Logger,
) server.Config {
	// Beta Logs Gate — ContextLogger enriches every handler's request-scoped
	// logger with correlation_id and, from the span OtelHTTP opened, trace_id.
	contextLogger := pkglogging.NewContextLogger(logger, observability.TraceIDs)

	// The environment handler is a builder whose three collaborators each come
	// from a dependency `main.go` can pass nil — so the chain has to be applied
	// step by step, guarded. Written inline in the struct literal below, every
	// `With*` argument is evaluated unconditionally, and `dbAdapter.Pool` on the
	// no-database boot is a nil dereference before any guard in this function
	// runs. See MEET-ASYNC-AMENDMENT-2026-08-16-no-database-boot-panics.
	//
	// A missing collaborator is not silently equivalent to a present one: the
	// project lookup below is the cross-tenant gate, and the handler falls open
	// without it. It is absent here only when there is no database to look in,
	// which is the same condition that makes the routes it guards unreachable.
	environmentHandler := environments.NewEnvironmentHandler(k8sDeps.TenantCRMgr)
	if dbAdapter != nil {
		environmentHandler = environmentHandler.WithKPILookup(&adapters.EnvKPILookup{DB: dbAdapter})
	}
	if stores != nil {
		environmentHandler = environmentHandler.WithPreviewSlotLookup(
			&adapters.PreviewSlotLookup{Store: stores.PreviewSlotStore},
		)
	}
	if dbDeps != nil {
		// M2 v1.2 T4.1 (LIVE-caught wiring gap 2026-06-08): the EnvProjectLookup
		// gate is REQUIRED in production so the EC-1 cross-tenant project check
		// actually fires. Without this, the handler falls open (test-fixture
		// mode) and a user from tenant A could create an env under a project
		// owned by tenant B. The adapter scopes the lookup by tenant_id via the
		// canonical GetByName query.
		environmentHandler = environmentHandler.WithProjectLookup(
			&adapters.EnvProjectLookupAdapter{Store: dbDeps.ProjectStore},
		)
	}

	sc := server.Config{
		Port:           cfg.Port,
		MetricsPort:    cfg.MetricsPort,
		Logger:         logger,
		ContextLogger:  contextLogger,
		HealthServices: healthServices,
		TokenValidator: tokenValidator,
		TenantCRMgr:    k8sDeps.TenantCRMgr,
		// Secrets.EnvLocker and Secrets.ProjectChecker are read off `stores` and
		// `dbDeps` inside the guards below — both are nil on the no-database
		// boot (`setupDatabase` returns (nil, nil) when DATABASE_URL is empty),
		// and reading them here panicked before the guards were ever reached.
		DomainHandler: domainHandler,
		BuildHandler:  buildHandler,
		// CHG-2026-04-25-ATTESTATION-WIRE Phase 6: wire concrete
		// PostgresAttestationStore so /api/v1/builds/{id}/attestation
		// returns real data (was always 404 because the store was nil).
		BuildAttestationHandler: builds.NewBuildAttestationHandler(
			store.NewPostgresAttestationStore(dbAdapter), logger,
		),
		// Sprint 6 / T1.2: per-app SBOM endpoint. The store is the
		// release_app_attestations sidecar (T0.1). The presigner is
		// the existing S3 uploader (loopzero-static-assets bucket / sbom/
		// prefix). When stores.ObjUploader is nil (no S3 in dev), the
		// handler still serves metadata but omits download_url.
		BuildSBOMHandler: builds.NewBuildSBOMHandler(
			&adapters.AppSBOMStore{Inner: store.NewAppAttestationStoreSqlc(dbAdapter)},
			toSBOMURLGenerator(stores),
			cfg.SourceBucket,
			logger,
		),
		EnvironmentHandler: environmentHandler,
		ShutdownCtx:        shutdownCtx,
		ShutdownCancel:     shutdownCancel,
		IsProduction:       cfg.IsProduction(),
		CORSAllowedOrigin:  cfg.CORSAllowedOrigin,
		Secrets:            server.SecretsConfig{K8sSync: k8sDeps.SecretK8sSync},
		Guards: server.RouteGuards{
			RateLimiter: rateLimiter,
			UpgradeURL:  os.Getenv("BILLING_UPGRADE_URL"),
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

	if dbDeps != nil {
		sc.MemberStore = dbDeps.MemberStore
		sc.ProjectStore = dbDeps.ProjectStore
		sc.DB = dbDeps.DBHealth
		sc.Guards.PlanLimiter = dbDeps.PlanLimiter
		if dbDeps.PlanLimiter != nil {
			sc.Guards.PlanUsage = store.NewPostgresPlanUsageStore(dbDeps.DBAdapter.Pool)
		}
		// T1.2 (plan 2026-06-10-j22-openbao-transit-eso-sync v1.2): the existing
		// *store.PostgresProjectStore doubles as the cross-tenant binding checker
		// (EC-03 absorption — 404 byte-identical between cross-tenant attempts and
		// nonexistent projects; CWE-204 defense). PostgresProjectStore.GetByName
		// already matches secrets.SecretProjectChecker, so no adapter is needed.
		sc.Secrets.ProjectChecker = dbDeps.ProjectStore
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
		costStore := store.NewSQLCostStore(dbAdapter)
		costChecker := enforcement.NewCapChecker(costStore)
		// Reuse the same SQL `subscriptions.plan` lookup PlanLimiter uses,
		// via a fresh NewSQLLimitsStore. PlanLimiter does not expose its
		// internal store, so we construct one with the same DB handle —
		// the query is centralized in enforcement.sqlLimitsStore so this
		// is single-source-of-truth.
		limitsStore := enforcement.NewSQLLimitsStore(dbDeps.StdDB)
		planReader := store.NewPlanReader(limitsStore)
		sc.Guards.CostEnforcer = policy.NewCostEnforcer(costChecker, planReader, logger)
		// Wire PlanTierStore into ScaleHandler + ProjectHandler so they
		// resolve the canonical tier (per ADR D4 of plan
		// 2026-06-15-code-review-49-findings-remediation). Without this,
		// scale.go:252-254 silently fallbacks to PlanTierStarter even
		// when subscriptions.plan = 'team' active. QA Run-3 (#123)
		// caught this — engine served `starter` despite DB `team`.
		// LimitsStore (enforcement.sqlLimitsStore) already implements
		// pkgdomain.PlanTierStore.GetTenantPlan(ctx, tenantID) →
		// (string, error) verbatim per pkg/enforcement/store.go:64.
		if orchDeps != nil && orchDeps.ScaleHandler != nil {
			orchDeps.ScaleHandler.SetPlanTierStore(limitsStore)
		}
		// StatusHandler wiring — #142 / #123 canonical of
		// qa-dogfood-cleanup-2026-06-21. GET /api/v1/projects/{name}/status
		// now surfaces the resolved tenant tier so the dashboard Scale
		// page can seed the slider ceiling pre-Apply (closes the gap
		// where the prior /v1/billing seed depended on Known Gap #116
		// cloud BFF billing read-side, which is 404).
		if orchDeps != nil && orchDeps.StatusHandler != nil {
			orchDeps.StatusHandler.SetPlanTierStore(limitsStore)
		}
		// ProjectHandler is constructed inline in router.go — surface
		// the LimitsStore via server.Config.PlanTierStore so the router
		// can wire it before registering /projects routes.
		sc.PlanTierStore = limitsStore
	}

	if stores != nil {
		sc.SourceStore = stores.SourceStore
		sc.ObjectUploader = stores.ObjUploader
		sc.SourceBucket = cfg.SourceBucket
		sc.EventStore = stores.EventStore
		// Sprint A T1.1 (closes comp-001 CRITICAL — secret mutations were running
		// without advisory lock). Reuses the same locker injected into build
		// orchestrators, so secret writes serialize with deploys on the same
		// (tenant, project, env) key. Lock-domain parity EC-2.
		sc.Secrets.EnvLocker = stores.Locker
		sc.BuildStoreReader = stores.BuildStoreReader
		// T5.1 — canonical Deploy Timeline read-store. Composes the
		// build-store reader (BuildLookup → tenant_id) and an SSE event
		// reader adapter into the routes.DeployTimelineEventStore contract.
		// nil-safe: when either dependency is nil, the route is omitted.
		if blu, ok := stores.BuildStoreReader.(aggregator.BuildLookup); ok && stores.EventStore != nil {
			sc.DeployTimelineStore = aggregator.NewPostgresDeployTimelineStore(
				blu,
				adapters.NewDeployTimelineSSEReader(stores.EventStore),
			)
		}
		sc.Secrets.Store = stores.SecretStore
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
		sc.StatusSSEHandler = orchDeps.StatusSSEHandler
		sc.ScaleHandler = orchDeps.ScaleHandler
		sc.BuildListHandler = orchDeps.BuildListHandler
		sc.BuildStatsHandler = orchDeps.BuildStatsHandler
		sc.ProjectDeleter = orchDeps.ProjectDeleter
	}

	if ghDeps != nil {
		sc.GitHubWebhookHandler = ghDeps.WebhookHandler
		sc.RepositoryHandler = ghDeps.RepoHandler
		sc.IntegrationsGitHubHandler = ghDeps.IntegrationsHandler
	}

	if dbAdapter != nil {
		sc.Guards.IdempotencyStore = &store.PostgresIdempotencyStore{DB: dbAdapter}
		sc.Secrets.AuditLogger = &store.AuditLogger{DB: dbAdapter}
		// T1.6 of deep-review-remediation: wire BillingHandler so
		// GET /api/v1/billing returns plan + usage instead of 404.
		// T1.9 production: also wire the cost cap CostStore so the
		// /api/v1/billing response includes the 3 metrics (egress,
		// docr_pulls, build_minutes) with their current values + caps.
		// pre-migration-000017 the costSource returns no rows and
		// usage map stays empty (graceful degradation).
		// The plan shown is the plan enforced: billing reads it through the same
		// store every plan gate uses (sc.PlanTierStore, set above when the
		// database is up), not through its own query.
		billingHandler := billing.NewBillingHandler(sc.PlanTierStore, store.NewSubscriptionStore(dbAdapter))
		if dbDeps != nil && dbDeps.StdDB != nil {
			// T2.2 #5 of sqlc-migration: dispatches via SqlcBridge (DBTX).
			billingHandler.WithCostSource(store.NewSQLCostStore(dbAdapter))
		}
		// THE ENGINE DOES NOT ISSUE PORTAL SESSIONS. Per ADR 2026-08-11
		// (docs/decisions.md), loopzero-cloud is the only
		// issuer: it is the one that CREATES the Stripe customer when it is missing
		// (EnsureStripeCustomer), and the engine only required that it already existed. Keeping
		// the capability here meant the Stripe key could live in the process that serves the CLI
		// and the critical deploy path — surface with nothing in return, since the preceding step
		// lives on the other side.
		//
		// `BillingHandler` keeps the `WithPortalSessionCreator` injector on purpose: it is the
		// handler's testable seam. What left was the IMPLEMENTATION that talks to Stripe.

		sc.BillingHandler = billingHandler
		// AUDIT-10: single PostgresTeamStore instance serves both team admin
		// endpoints and /whoami username lookup.
		teamStore := &store.PostgresTeamStore{DB: dbAdapter}
		sc.TeamHandler = teams.NewTeamHandler(teamStore)
		sc.AuthUserLookup = teamStore
		// Plan T2.5 D8 full — persistent active-team selection backed by
		// users.active_tenant_id column (migration 000018).
		sc.ActiveTeamStore = store.NewPostgresActiveTeamStore(dbAdapter)
		sc.AuditHandler = audit.NewAuditHandler(&store.PostgresAuditStore{DB: dbAdapter})
		// B-117 — the soft-deletion cycle (MEET-052). Without this assignment the
		// router's `if cfg.TenantDeletionStore != nil` guard is never true, and
		// DELETE /tenants/{id}, POST /tenants/{id}/restore and
		// POST /tenants/bulk-delete exist in no deployment at all — while the
		// dashboard's account-deletion screen really calls the first of them.
		//
		// It stayed that way for months: the sqlc pilot created the store and deleted
		// the legacy one in the same commit without wiring the new one, so it was born
		// unreachable and a quality pass removed it as dead code three weeks later.
		sc.TenantDeletionStore = store.NewTenantDeletionStoreSqlc(dbAdapter)
		sc.Admin.Evictor = buildAdminEvictor(cfg, dbAdapter)
		// CHG-2026-04-30-DELETE-SINGLE-CELL: wire the CellPinner so the
		// /api/v1/admin/tenants/pin endpoint registered in router.go can
		// actually serve requests. Previously the handler existed but had
		// no consumer, returning 404. loopzero admin tenants pin --cell <id>
		// is the only path to override automatic cell assignment for a
		// tenant.
		// CHG-2026-04-30-DELETE-SINGLE-CELL: pinner also patches Tenant CR
		// so operator reconcile re-emits ArgoCD destination with new cell.
		pinner := placementsvc.NewCellPinner(&store.PostgresCellStore{DB: dbAdapter})
		if mgmt := buildMgmtDynamicClient(cfg, logger); mgmt != nil {
			pinner = pinner.WithTenantCRPatcher(adapters.NewTenantCRCellPatcher(mgmt, "loopzero-system"))
		}
		sc.Admin.Pinner = pinner
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
		sc.Admin.ReleaseInspector = release.NewReleaseInspector(
			store.NewReleaseStoreSqlc(dbAdapter, store.NoopEventStore{}),
			&store.PostgresAuditStore{DB: dbAdapter},
			nil,
		)
		sc.AuditWriter = func(tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) {
			entry := store.AuditEntry{
				TenantID: tenantID, Action: action, Actor: actor,
				ResourceType: resourceType, ResourceID: resourceID,
				CorrelationID: correlationID, Metadata: metadata,
			}
			if err := store.WriteAuditLog(context.Background(), dbAdapter, entry); err != nil {
				logger.Warn("audit write failed", "error", err)
			}
		}
		// B-054: the pool gate. Until 2026-08-19 this point handed over a statistics function
		// and the router computed an instantaneous percentage on every request — which oscillates
		// around the cut-off line. The `PGPoolBreaker` already existed, with hysteresis, built and
		// started in `setupBuildHandler`, and was NEVER consulted: the instrument that solved the
		// oscillation was switched off and the one that decided had the problem.
		pgGate := admission.NewPGPoolBreaker(dbAdapter.Pool, admission.DefaultBreakerConfig(cfg.DBMaxConns), logger)
		pgGate.Start(shutdownCtx)
		go func() {
			<-shutdownCtx.Done()
			pgGate.Stop()
		}()
		sc.Guards.PGPoolGate = pgGate
		logger.Info("pg pool gate enabled",
			"high_water", 0.90, "low_water", 0.75, "max_conns", cfg.DBMaxConns)
		// Sprint 5 / T2.3 + T1.1 — Preview environments handler + service.
		// The handler exposes /api/v1/projects/{project}/previews; the
		// service drives the dual-write (CR + DB) per ADR D7. When the
		// BuildOrchestrator is available (always, post-Sprint-2), the
		// service can submit builds for previews (best-effort).
		previewStore := store.NewPostgresPreviewStore(dbAdapter, dbAdapter)
		var previewService *previewsvc.PreviewService
		if orchDeps != nil && orchDeps.Orchestrator != nil {
			ps, perr := previewsvc.NewPreviewService(previewsvc.PreviewServiceDeps{
				Store:               previewStore,
				Builds:              orchDeps.Orchestrator,
				BuildApps:           &adapters.PreviewAppsLookupAdapter{Builds: stores.BuildStore},
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
		//
		// Red Team loopzero-cloud#92 absorption (2026-06-27): wire the
		// ProjectChecker so /previews handlers reject non-existent
		// projects with 404 PROJECT_NOT_FOUND instead of 200 + empty
		// list. Existing *store.PostgresProjectStore matches the narrow
		// interface signature — no adapter needed.
		sc.PreviewHandler = previewroutes.
			NewPreviewHandler(previewStore, previewService, logger).
			WithPhaseLookup(k8sDeps.PreviewCRMgr).
			WithProjectChecker(&store.PostgresProjectStore{DB: dbAdapter})
		sc.CellPlacement = func() ports.CellPlacementEngine {
			engine := placement.NewRoundRobinEngine(cellRouter, cellStore, cfg.MaxTenantsPerCell)
			return engine.WithSerializer(placement.NewAdvisoryLockSerializer(dbAdapter.Pool))
		}()
		if checker, ok := cellRouter.(health.CellDiscoveryChecker); ok {
			sc.CellDiscovery = checker
		}
	}

	return sc
}

// toSBOMURLGenerator returns a SBOMURLGenerator if the configured
// uploader supports presigning, else nil. When nil, the SBOM endpoint
// returns metadata-only (no download_url).
func toSBOMURLGenerator(stores *StoreDeps) builds.SBOMURLGenerator {
	if stores == nil {
		return nil
	}
	if g, ok := stores.ObjUploader.(builds.SBOMURLGenerator); ok {
		return g
	}
	return nil
}
