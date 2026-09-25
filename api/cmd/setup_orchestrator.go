// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/getloopscale/loopzero/api/internal/logs/logshttp"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/projectmetrics/projectmetricshttp"
	"github.com/getloopscale/loopzero/api/internal/routes/argocd"
	"github.com/getloopscale/loopzero/api/internal/routes/builds"
	"github.com/getloopscale/loopzero/api/internal/routes/promote"
	"github.com/getloopscale/loopzero/api/internal/routes/rollback"
	"github.com/getloopscale/loopzero/api/internal/status/statushttp"

	"github.com/getloopscale/loopzero/api/internal/services/build"

	"github.com/getloopscale/loopzero/api/internal/services/release"

	"github.com/getloopscale/loopzero/api/internal/services/platform"

	"github.com/getloopscale/loopzero/api/internal/services/scaling"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/adapters"
	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/logs/loki"
	"github.com/getloopscale/loopzero/api/internal/routes/scale"
	"github.com/getloopscale/loopzero/pkg/enforcement"
)

// planLimiterCapAdapter bridges *enforcement.PlanLimiter to the
// build.PlanCapChecker interface required by BuildOrchestrator (B15).
// Keeps the orchestrator package free of pkg/enforcement imports (DIP).
type planLimiterCapAdapter struct {
	limiter *enforcement.PlanLimiter
}

func (a *planLimiterCapAdapter) CheckReplicasValue(ctx context.Context, tenantID string, requested int) (bool, string, int, error) {
	if a == nil || a.limiter == nil {
		return true, "dev", 0, nil
	}
	result, err := a.limiter.CheckValue(ctx, tenantID, "max_replicas_per_service", requested)
	if err != nil {
		return false, "", 0, err
	}
	return result.Allowed, result.Plan, result.Limit, nil
}

// CheckProjectsValue resolves the per-Account project cap (M4). Mirrors
// CheckReplicasValue; satisfies store.ProjectCapChecker so the project-creation
// store can enforce the cap atomically. Returns (allowed, plan, cap) where
// cap < 0 = unlimited.
func (a *planLimiterCapAdapter) CheckProjectsValue(ctx context.Context, tenantID string, current int) (bool, string, int, error) {
	if a == nil || a.limiter == nil {
		return true, "dev", -1, nil // dev/no-limiter → unlimited (no enforcement)
	}
	result, err := a.limiter.CheckProjectsValue(ctx, tenantID, current)
	if err != nil {
		return false, "", 0, err
	}
	return result.Allowed, result.Plan, result.Limit, nil
}

// projectCapEnforcement is everything a project store needs to enforce the
// per-Account project cap, carried as ONE value so no store can be wired with
// half of it. Both halves are load-bearing:
//
//   - checker resolves the plan's cap (nil = unenforced, dev without a pool);
//   - crossTenant counts the Account's projects across ITS tenants. The request
//     pool is RLS-scoped to one tenant since migration 000062, so a store
//     counting through it turns a per-Account cap into a per-tenant one.
//
// planCap is the same adapter seen through the scaling port, so the build
// orchestrator's replica cap and its project cap come from one limiter.
type projectCapEnforcement struct {
	checker     store.ProjectCapChecker
	crossTenant db.DBTX
	planCap     build.PlanCapChecker
}

// newProjectCapEnforcement builds the enforcement from the plan limiter and the
// cross-tenant (loopzero_worker) connection. A nil limiter yields the zero value:
// no enforcement, which is the dev-without-pool path.
func newProjectCapEnforcement(limiter *enforcement.PlanLimiter, crossTenant db.DBTX) projectCapEnforcement {
	if limiter == nil {
		return projectCapEnforcement{}
	}
	adapter := &planLimiterCapAdapter{limiter: limiter}
	return projectCapEnforcement{checker: adapter, crossTenant: crossTenant, planCap: adapter}
}

// wireProjectCap is the SINGLE path that wires the per-Account project cap onto
// a project store. Both project-creating stores — the API store and the
// build-orchestrator store — MUST go through here with the same
// projectCapEnforcement, so they can never diverge again: the divergence (one
// store capped, one not — then one store counting across tenants, one not) was
// the root cause of #106. A nil checker leaves the store unenforced.
func wireProjectCap(s *store.PostgresProjectStore, e projectCapEnforcement) {
	if s == nil || e.checker == nil {
		return
	}
	s.SetProjectCapChecker(e.checker)
	if e.crossTenant != nil {
		s.SetCrossTenantCounter(e.crossTenant)
	}
}

// OrchestratorDeps holds the build orchestrator and related handlers.
type OrchestratorDeps struct {
	Orchestrator        *build.BuildOrchestrator
	BuildOrchestrator   builds.BuildOrchestratorService
	LogsHandler         *logshttp.LogsHandler
	LogStreamHandler    *logshttp.LogStreamHandler
	RollbackHandler     *rollback.RollbackHandler
	PromoteHandler      *promote.PromoteHandler
	StatusHandler       *statushttp.StatusHandler
	MetricsProxyHandler *projectmetricshttp.MetricsProxyHandler // T1.2 J-18 — Prometheus query_range proxy (nil = degraded 503)
	StatusSSEHandler    *statushttp.StatusSSEHandler            // T2.1 J-18 T1.3.b — SSE signal.changed stream
	ScaleHandler        *scale.ScaleHandler
	BuildListHandler    *builds.BuildListHandler
	BuildStatsHandler   *builds.BuildStatsHandler // J-06 D2 — Overview deploys aggregate
	ProjectDeleter      ports.ProjectDeleter
	ArgoCallbackHandler *argocd.ArgoCallbackHandler
}

// setupOrchestrator creates the build orchestrator, logs, rollback, status,
// build list handlers, project deleter, and ArgoCD callback handler.
func setupOrchestrator(
	dbAdapter *db.PgxAdapter,
	stores *StoreDeps,
	workflows *WorkflowDeps,
	cfg config.Config,
	httpTransport *http.Transport,
	logger *slog.Logger,
	projectCap projectCapEnforcement,
	envEnsurer *platform.EnvironmentEnsurer,
) *OrchestratorDeps {
	// The orchestrator builds its OWN project store for the deploy-time
	// EnsureProject path (`loopzero deploy <new-name>`). It is wired through the same
	// wireProjectCap and the same projectCapEnforcement as the API store, so the
	// deploy path enforces the identical per-Account cap, counted across the
	// Account's tenants. A zero projectCap (dev without a pool) leaves both the
	// store and the scaling PlanCap unenforced; the orchestrator accepts nil.
	buildProjectStore := &store.PostgresProjectStore{DB: dbAdapter}
	wireProjectCap(buildProjectStore, projectCap)
	planCap := projectCap.planCap

	// B40 (CHG-2026-05-05-DOGFOOD-B40): instantiated BEFORE BuildOrchestrator
	// because the orchestrator depends on it via Deps.ScalingResolver. The
	// `scaleApply` closure below reuses the same scalingStore (single source
	// of truth for app_scaling_config IO).
	scalingStore := store.NewPostgresScalingStore(dbAdapter)
	scalingResolver, err := scaling.NewDBScalingResolver(scalingStore)
	if err != nil {
		// Unreachable with the store built on the line above; a wiring change
		// that breaks it must stop the process rather than deploy the yaml's
		// scaling while ignoring every persisted config.
		logger.Error("setup orchestrator: scaling resolver", "error", err)
		os.Exit(1)
	}

	// One container delivery activator serves both build activation and
	// rollback, so the two write through the same outbox and read manifests
	// back with the same reader. It used to be built inside NewBuildOrchestrator
	// as a fallback, which hid a concrete sqlc store in the build package and
	// left rollback on a separate inline copy of the same delivery steps.
	delivery := release.NewContainerDeliveryActivator(release.ContainerDeliveryDeps{
		ReleaseStore:      stores.ReleaseStore,
		EnvTargetStore:    stores.EnvTargetStore,
		EventStore:        stores.PgEventStore,
		Sync:              workflows.SyncClient,
		Locker:            stores.Locker,
		GitOps:            workflows.GitOpsPusher,
		GitReader:         workflows.GitOpsPusher,
		ArgocdSyncTimeout: cfg.ArgocdSyncTimeout,
		// Sprint 6 / T3.1: per-app attestation persistence (signature + SBOM).
		// T1.3b: the sqlc-backed wrapper preserves the ALL-signed atomicity
		// invariant + F-S2-01 FK race closure (SaveAppAttestationsTx requires a
		// non-nil tx for the atomic release+attestation commit).
		AppAttestationStore: store.NewAppAttestationStoreSqlc(dbAdapter),
		// J-16 slice 3: deploy.succeeded outbox.
		DeployEventStore: store.NewDeployEventStoreSqlc(),
	})

	orch := build.NewBuildOrchestrator(build.BuildOrchestratorDeps{
		BuildStore:        stores.BuildStore,
		ReleaseStore:      stores.ReleaseStore,
		EnvTargetStore:    stores.EnvTargetStore,
		ProjectStore:      buildProjectStore, // M5: cap-checker-wired store (closes #106)
		EventStore:        stores.PgEventStore,
		Workflow:          workflows.WorkflowSubmitter,
		Sync:              workflows.SyncClient,
		Locker:            stores.Locker,
		GitOps:            workflows.GitOpsPusher,
		Delivery:          delivery,
		RegistryURL:       cfg.RegistryURL,
		ArgocdSyncTimeout: cfg.ArgocdSyncTimeout,
		KVClient:          workflows.KVClient,
		BaseDomain:        cfg.BaseDomain,
		PlanCap:           planCap,
		ScalingResolver:   scalingResolver, // B40
		// T1.2 mitigation #3 of MEET-2026-05-12: per-sign cosign audit
		// log. AuditLogger reuses WriteAuditLog as the canonical SQL.
		AuditLogger: &store.AuditLogger{DB: dbAdapter},
		// Plan deploy-timeline-canonical-5-phases T5.1 — per-deploy
		// 5-phase aggregator spawn hook. Wired to a release_db-only
		// minimal source set as the canonical v1 — the other 4 source
		// adapters (loki_markers + argocd_app + loopzeroapp_informer +
		// status_url_probe) are documented as the v1.1 production
		// follow-up per the plan's Open Questions section. Nil-safe.
		DeployTimelineSpawn: makeDeployTimelineSpawn(stores.PgEventStore),
		// J-16 slice 3 (MEET-2026-07-04-001): deploy.failed outbox emit on the
		// build-completed FAILED path.
		DeployEventStore: store.NewDeployEventStoreSqlc(),
	})

	buildOrchestrator := &adapters.BuildOrchestratorAdapter{
		Orch:       orch,
		BuildStore: stores.BuildStore,
	}

	// Build logs via Loki — uses a dedicated HTTP client with longer timeout.
	// Loki queries cross-cluster (mgmt → build NodePort) can take 15-30s for
	// large builds. The default httpClient has 10s timeout which causes
	// "context deadline exceeded" on most queries.
	lokiHTTPClient := &http.Client{
		Timeout:   30 * time.Second,
		Transport: httpTransport,
	}
	lokiClient := loki.NewClient(cfg.LokiURL, lokiHTTPClient, logger)
	logBuildStore := &adapters.LogBuildStoreAdapter{Store: stores.BuildStore}
	logsHandler := logshttp.NewLogsHandler(logBuildStore, lokiClient)
	logStreamHandler := logshttp.NewLogStreamHandler(logBuildStore, lokiClient, logshttp.LogStreamHandlerOpts{
		Logger: logger,
	})

	// Rollback orchestrator — coordinates release rollback with lock + state machine.
	// Re-delivery goes through the shared container delivery activator, which
	// waits for sync confirmation (bounded by cfg.ArgocdSyncTimeout) before the
	// orchestrator flips release state — the LIVE sponsor mandate of 2026-06-18.
	rollbackOrch := release.NewRollbackOrchestrator(release.RollbackOrchestratorDeps{
		Releases:     stores.ReleaseStore,
		EnvTargets:   stores.EnvTargetStore,
		Locker:       stores.Locker,
		Delivery:     delivery,
		PublicDomain: cfg.PublicDomain,
		Logger:       logger,
	})
	rollbackService := &adapters.RollbackServiceAdapter{Orchestrator: rollbackOrch}
	projectStore := &store.PostgresProjectStore{DB: dbAdapter}
	rollbackHandler := rollback.NewRollbackHandler(projectStore, rollbackService, cfg.BaseDomain).
		WithReleaseLookup(&adapters.RollbackReleaseLookupAdapter{Store: stores.ReleaseStore})

	// Promotion orchestrator — coordinates release promotion across environments
	promotionOrch := release.NewPromotionOrchestrator(release.PromotionOrchestratorDeps{
		Releases:   stores.ReleaseStore,
		EnvTargets: stores.EnvTargetStore,
		GitReader:  workflows.GitOpsPusher,
		GitPusher:  workflows.GitOpsPusher,
		Sync:       workflows.SyncClient,
		Locker:     stores.Locker,
		BaseDomain: cfg.BaseDomain,
		// 2026-06-30 (closes Red Team loopzero-cloud#120 sub-finding 3 — #61
		// regression): wire cfg.PublicDomain so the promote response URL is
		// the canonical user-facing host (prod-dev.loopzero.example) not the
		// internal placeholder (loopzero.local). Empty PublicDomain falls back
		// to BaseDomain inside the orchestrator (back-compat for local
		// Tilt dev where LOOPZERO_PUBLIC_DOMAIN is unset). Mirrors the canonical
		// pattern already applied to ReleaseConvergenceLoop + SyncCallbackActivator.
		PublicDomain: cfg.PublicDomain,
		Logger:       logger,
	})
	promoteService := &adapters.PromoteServiceAdapter{Orchestrator: promotionOrch}
	promoteHandler := promote.NewPromoteHandler(projectStore, promoteService, cfg.BaseDomain)
	if envEnsurer != nil {
		// promote --to <new env> creates the target env like deploy does (#158).
		promoteHandler = promoteHandler.WithEnvironmentEnsurer(envEnsurer)
	}

	// Status handler — wires project, build, and release stores (H6 status endpoint)
	statusBuildStore := &adapters.StatusBuildStoreAdapter{Store: stores.BuildStore}
	statusReleaseStore := &adapters.StatusReleaseStoreAdapter{Store: stores.ReleaseStore}
	statusHandler := statushttp.NewStatusHandler(projectStore, statusBuildStore, statusReleaseStore, cfg.BaseDomain, cfg.PublicDomain, cfg.GrafanaBaseURL)

	// T1.2 J-18 — Prometheus query_range proxy for the Status page KPI strip +
	// SignalDetailSheet histograms. When LOOPZERO_PROMETHEUS_ADDRESS is set, wire the
	// concrete HTTP client (mirrors the loki.NewClient wiring above); otherwise
	// ship nil so the handler returns 503 prometheus_not_configured honestly
	// (plan v1.1 ADR D2 — degraded UI is the canonical disabled state). Hermetic
	// Go tests inject httptest-backed fakes.
	var promClient projectmetricshttp.PrometheusQueryClient
	if cfg.PrometheusAddress != "" {
		promClient = projectmetricshttp.NewPrometheusClient(cfg.PrometheusAddress, nil)
	}
	metricsProxyHandler := projectmetricshttp.NewMetricsProxyHandler(projectStore, promClient)

	// T2.1 J-18 T1.3.b — SSE signal.changed stream handler, on the concrete
	// PostgresEventStore (constructed in setup_stores.go): only it reads the
	// environment-scoped signals, which are keyed by release id.
	statusSSEHandler := statushttp.NewStatusSSEHandler(projectStore, stores.PgEventStore, statushttp.StatusSSEHandlerOpts{})

	// Scale handler — scaling configuration for apps (KEDA scale-to-zero gate).
	// Plan T2.2: real persistence via PostgresScalingStore (was: stub-202 returning acknowledged).
	// The closure adapts scale.ScaleConfigInput ↔ store.ScalingConfig (avoids circular import).
	// scalingStore declared above (before BuildOrchestrator) — single instance
	// shared by scaleApply (write path) and ScalingResolver (read path).
	// T7.1 of deep-review-remediation-plan (closes find_data_003 HIGH).
	// CLAUDE.md Rule 6 requires every (tenant, project, environment) mutation
	// to acquire withEnvironmentLock. Previously, scaleApply called
	// scalingStore.Upsert(dbAdapter, ...) directly, bypassing the lock.
	// Two concurrent `loopzero scale` calls on the same env could TOCTOU-race the
	// read-current/write-new sequence inside Upsert. Now wrapped in WithLock
	// so concurrent attempts serialize via pg_advisory_xact_lock.
	scaleApply := func(ctx context.Context, in scale.ScaleConfigInput) (bool, error) {
		var changed bool
		err := stores.Locker.WithLock(ctx, in.TenantID, in.ProjectID, in.Environment, func(tx db.DBTX) error {
			c := &store.ScalingConfig{
				TenantID:     in.TenantID,
				ProjectID:    in.ProjectID,
				Environment:  in.Environment,
				AppName:      in.AppName,
				MinReplicas:  in.MinReplicas,
				MaxReplicas:  in.MaxReplicas,
				TargetCPUPct: in.TargetCPUPct,
				ScaleToZero:  in.ScaleToZero,
				UpdatedBy:    in.UpdatedBy,
			}
			_, ch, upErr := scalingStore.Upsert(ctx, tx, c)
			if upErr != nil {
				return upErr
			}
			changed = ch
			return nil
		})
		return changed, err
	}
	scaleHandler := scale.NewScaleHandler(projectStore, scaleApply, cfg.KEDAEnabled)
	// Red Team loopzero-cloud#101 absorption (2026-06-27): wire the same
	// plan-cap checker the build path uses so the scale endpoint can
	// reject Starter:replicas:999 (and equivalent per-tier overshoots).
	// Without this, the scale path silently bypassed the cap.
	if planCap != nil {
		scaleHandler.SetReplicasChecker(planCap)
	} else {
		// Issue #137 — planCap is nil only when there is no database, hence no
		// PlanLimiter and no plan to enforce. Say so explicitly: the guard below
		// must be able to tell this from a dropped SetReplicasChecker call.
		scaleHandler.WaiveReplicaCapEnforcement("no database in this boot — no PlanLimiter, so no plan to enforce")
		logger.Warn("scale: per-plan replica cap DISABLED — no PlanLimiter in this boot (no database); tenants may request any replica count")
	}
	// audit M02 #14 + issue #137: panic-on-regression guard. MUST run AFTER the
	// wiring above — it asserts both the apply closure and a decision about cap
	// enforcement, and neither exists before this point.
	scaleHandler.EnsureProductionWired()

	// Build list handler — lists builds for a project with optional env
	// filter PLUS exposes GET /api/v1/projects/{name}/build-envs (T2.2).
	// Plan 2026-06-08-dashboard-screens-review-fixes T2.2 + EC-2
	// SEPA-CRITICAL: NewBuildListHandlerWithGate wires the ProjectStore
	// so the /build-envs endpoint can reject cross-tenant probes with
	// 404 PROJECT_NOT_FOUND (existence-leak prevention) BEFORE the
	// underlying ListEnvsForProject query runs.
	buildListHandler := builds.NewBuildListHandlerWithGate(
		&adapters.BuildListStoreAdapter{Store: stores.BuildStore},
		projectStore,
	)

	// J-06 D2: the Overview deploys-aggregate needs the concrete sqlc store
	// (BuildStatsByDay is not on the db.BuildStore interface — ISP). A fresh
	// BuildStoreSqlc off the same dbAdapter mirrors main.go's admission-querier.
	buildStatsHandler := builds.NewBuildStatsHandler(
		&adapters.BuildStatsStoreAdapter{Store: store.NewBuildStoreSqlc(dbAdapter)},
	)

	// Project deleter — orchestrates project removal across DB, GitOps, and builds.
	// WorkflowCanceller stops running Argo Workflows during deletion (DF-2.1).
	projectDeleter := platform.NewProjectDeleter(platform.ProjectDeleterDeps{
		BuildStore:        stores.BuildStore,
		EnvTargetStore:    stores.EnvTargetStore,
		ReleaseStore:      stores.ReleaseStore,
		ProjectStore:      &store.PostgresProjectStore{DB: dbAdapter},
		ScalingStore:      scalingStore,
		Locker:            stores.Locker,
		GitOps:            workflows.GitOpsPusher,
		WorkflowCanceller: workflows.WorkflowSubmitter,
		Sync:              workflows.SyncClient,
	})

	// ArgoCD sync callback activator — processes webhook from ArgoCD Notifications.
	// Push-based activation: ArgoCD notifies when sync completes instead of API polling.
	// release-blockers-2026-06-25-set v1.1 #61: cfg.PublicDomain is the
	// canonical public wildcard (LOOPZERO_PUBLIC_DOMAIN) — same source
	// status.go uses — so deploy.completed SSE URL matches `loopzero status`.
	// Empty PublicDomain → fall back to BaseDomain (preserves pre-#61
	// behavior for envs without a public override).
	syncCallbackActivator := release.NewSyncCallbackActivator(
		stores.ReleaseStore, stores.EnvTargetStore, stores.Locker, stores.EventStore, cfg.BaseDomain, cfg.PublicDomain, logger,
	).WithDeployEventStore(store.NewDeployEventStoreSqlc()) // J-16 slice 3 — deploy.succeeded outbox
	// The activator identifies the Application's tenant and environment from its
	// loopzero.io/{tenant,environment} labels — the only decoder, since the
	// "{tenant}-{project}-{env}" name cannot be split positionally. Without the
	// mgmt dynamic client the callback cannot identify releases and activation
	// falls to the convergence loop.
	if mgmtDynamic := buildMgmtDynamicClient(cfg, logger); mgmtDynamic != nil {
		syncCallbackActivator = syncCallbackActivator.WithAppLabelReader(
			adapters.NewDynamicArgoAppLabelReader(mgmtDynamic, "argocd"),
		)
		logger.Info("sync callback activator: label-based lookup wired")
	} else {
		logger.Warn("sync callback activator: dynamic client unavailable; callback activation disabled, the convergence loop activates releases")
	}
	argoCallbackHandler := argocd.NewArgoCallbackHandler(syncCallbackActivator, logger, cfg.ArgoCallbackSecret)

	return &OrchestratorDeps{
		Orchestrator:        orch,
		BuildOrchestrator:   buildOrchestrator,
		LogsHandler:         logsHandler,
		LogStreamHandler:    logStreamHandler,
		RollbackHandler:     rollbackHandler,
		PromoteHandler:      promoteHandler,
		StatusHandler:       statusHandler,
		MetricsProxyHandler: metricsProxyHandler,
		StatusSSEHandler:    statusSSEHandler,
		ScaleHandler:        scaleHandler,
		BuildListHandler:    buildListHandler,
		BuildStatsHandler:   buildStatsHandler,
		ProjectDeleter:      projectDeleter,
		ArgoCallbackHandler: argoCallbackHandler,
	}
}
