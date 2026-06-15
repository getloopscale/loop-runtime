// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/loopzero/loopzero/api/internal/adapters"
	"github.com/loopzero/loopzero/api/internal/config"
	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/loki"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/pkg/enforcement"
)

// planLimiterCapAdapter bridges *enforcement.PlanLimiter to the
// services.PlanCapChecker interface required by BuildOrchestrator (B15).
// Keeps the orchestrator package free of pkg/enforcement imports (DIP).
type planLimiterCapAdapter struct {
	limiter *enforcement.PlanLimiter
}

func (a *planLimiterCapAdapter) CheckReplicasValue(tenantID string, requested int) (bool, string, int) {
	if a == nil || a.limiter == nil {
		return true, "dev", 0
	}
	result := a.limiter.CheckValue(tenantID, "max_replicas_per_service", requested)
	return result.Allowed, result.Plan, result.Limit
}

// OrchestratorDeps holds the build orchestrator and related handlers.
type OrchestratorDeps struct {
	Orchestrator      *services.BuildOrchestrator
	BuildOrchestrator routes.BuildOrchestratorService
	LogsHandler         *routes.LogsHandler
	LogStreamHandler    *routes.LogStreamHandler
	RollbackHandler   *routes.RollbackHandler
	PromoteHandler    *routes.PromoteHandler
	StatusHandler       *routes.StatusHandler
	MetricsProxyHandler *routes.MetricsProxyHandler // T1.2 J-18 — Prometheus query_range proxy (nil = degraded 503)
	StatusSSEHandler    *routes.StatusSSEHandler    // T2.1 J-18 T1.3.b — SSE signal.changed stream
	ScaleHandler        *routes.ScaleHandler
	BuildListHandler  *routes.BuildListHandler
	ProjectDeleter    routes.ProjectDeleter
	ArgoCallbackHandler *routes.ArgoCallbackHandler
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
	planLimiter *enforcement.PlanLimiter,
) *OrchestratorDeps {
	// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059): wire plan-cap enforcement.
	// A nil limiter is legal — means the DB was unavailable (dev without pool)
	// and enforcement is off. The orchestrator also accepts a nil PlanCap.
	var planCap services.PlanCapChecker
	if planLimiter != nil {
		planCap = &planLimiterCapAdapter{limiter: planLimiter}
	}

	// B40 (CHG-2026-05-05-DOGFOOD-B40): instantiated BEFORE BuildOrchestrator
	// because the orchestrator depends on it via Deps.ScalingResolver. The
	// `scaleApply` closure below reuses the same scalingStore (single source
	// of truth for app_scaling_config IO).
	scalingStore := db.NewPostgresScalingStore(dbAdapter)
	scalingResolver := services.NewDBScalingResolver(scalingStore)

	orch := services.NewBuildOrchestrator(services.BuildOrchestratorDeps{
		BuildStore:        stores.BuildStore,
		ReleaseStore:      stores.ReleaseStore,
		EnvTargetStore:    stores.EnvTargetStore,
		ProjectStore:      &db.PostgresProjectStore{DB: dbAdapter},
		EventStore:        stores.PgEventStore,
		Workflow:          workflows.WorkflowSubmitter,
		Sync:              workflows.SyncClient,
		Locker:            stores.Locker,
		GitOps:            workflows.GitOpsPusher,
		RegistryURL:       cfg.RegistryURL,
		ArgocdSyncTimeout: cfg.ArgocdSyncTimeout,
		KVClient:          workflows.KVClient,
		BaseDomain:        cfg.BaseDomain,
		PlanCap:           planCap,
		ScalingResolver:   scalingResolver, // B40
		// Sprint 6 / T3.1: per-app attestation persistence (signature + SBOM).
		// T1.3b (plan 2026-05-29-sqlc-migration v1.3): sqlc-backed wrapper
		// satisfies db.AppAttestationStore 1:1 — preserves ALL-signed
		// atomicity invariant + F-S2-01 FK race closure (SaveAppAttestations
		// Tx requires non-nil tx for atomic release+attestation commit).
		AppAttestationStore: db.NewAppAttestationStoreSqlc(dbAdapter),
		// T1.2 mitigation #3 of MEET-2026-05-12: per-sign cosign audit
		// log. AuditLogger reuses WriteAuditLog as the canonical SQL.
		AuditLogger: &db.AuditLogger{DB: dbAdapter},
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
	logsHandler := routes.NewLogsHandler(logBuildStore, lokiClient)
	logStreamHandler := routes.NewLogStreamHandler(logBuildStore, lokiClient, routes.LogStreamHandlerOpts{
		Logger: logger,
	})

	// Rollback orchestrator — coordinates release rollback with lock + state machine
	rollbackOrch := services.NewRollbackOrchestrator(services.RollbackOrchestratorDeps{
		Releases:   stores.ReleaseStore,
		EnvTargets: stores.EnvTargetStore,
		GitReader:  workflows.GitOpsPusher,
		GitPusher:  workflows.GitOpsPusher,
		Sync:       workflows.SyncClient,
		Locker:     stores.Locker,
		Logger:     logger,
	})
	rollbackService := &adapters.RollbackServiceAdapter{Orchestrator: rollbackOrch}
	projectStore := &db.PostgresProjectStore{DB: dbAdapter}
	rollbackHandler := routes.NewRollbackHandler(projectStore, rollbackService, cfg.BaseDomain)

	// Promotion orchestrator — coordinates release promotion across environments
	promotionOrch := services.NewPromotionOrchestrator(services.PromotionOrchestratorDeps{
		Releases:   stores.ReleaseStore,
		EnvTargets: stores.EnvTargetStore,
		GitReader:  workflows.GitOpsPusher,
		GitPusher:  workflows.GitOpsPusher,
		Sync:       workflows.SyncClient,
		Locker:     stores.Locker,
		BaseDomain: cfg.BaseDomain,
		Logger:     logger,
	})
	promoteService := &adapters.PromoteServiceAdapter{Orchestrator: promotionOrch}
	promoteHandler := routes.NewPromoteHandler(projectStore, promoteService, cfg.BaseDomain)

	// Status handler — wires project, build, and release stores (H6 status endpoint)
	statusBuildStore := &adapters.StatusBuildStoreAdapter{Store: stores.BuildStore}
	statusReleaseStore := &adapters.StatusReleaseStoreAdapter{Store: stores.ReleaseStore}
	statusHandler := routes.NewStatusHandler(projectStore, statusBuildStore, statusReleaseStore, cfg.BaseDomain, cfg.PublicDomain, cfg.GrafanaBaseURL)

	// T1.2 J-18 — Prometheus query_range proxy for Status page histogram
	// rendering. Production adapter wiring deferred to follow-up (HTTP
	// client targeting cfg.PrometheusAddress); for now ship with nil
	// promClient so handler returns 503 prometheus_not_configured honestly
	// (per plan v1.1 ADR D2 — degraded UI is the canonical disabled state).
	// Hermetic Go tests use httptest-backed fakePrometheusQueryClient.
	metricsProxyHandler := routes.NewMetricsProxyHandler(projectStore, nil)

	// T2.1 J-18 T1.3.b — SSE signal.changed stream handler.
	// Wires canonical sse.EventStore (already constructed in setup_stores.go)
	// to project-scoped SSE endpoint. Handler enforces tenant ownership +
	// env validation + Flusher safety + Last-Event-ID dedup per plan
	// 2026-06-15-j18-t1.3.b-sse-pg-notify-backend v1.1.
	statusSSEHandler := routes.NewStatusSSEHandler(projectStore, stores.EventStore, routes.StatusSSEHandlerOpts{})

	// Scale handler — scaling configuration for apps (KEDA scale-to-zero gate).
	// Plan T2.2: real persistence via PostgresScalingStore (was: stub-202 returning acknowledged).
	// The closure adapts routes.ScaleConfigInput ↔ db.ScalingConfig (avoids circular import).
	// scalingStore declared above (before BuildOrchestrator) — single instance
	// shared by scaleApply (write path) and ScalingResolver (read path).
	// T7.1 of deep-review-remediation-plan (closes find_data_003 HIGH).
	// CLAUDE.md Rule 6 requires every (tenant, project, environment) mutation
	// to acquire withEnvironmentLock. Previously, scaleApply called
	// scalingStore.Upsert(dbAdapter, ...) directly, bypassing the lock.
	// Two concurrent `loopzero scale` calls on the same env could TOCTOU-race the
	// read-current/write-new sequence inside Upsert. Now wrapped in WithLock
	// so concurrent attempts serialize via pg_advisory_xact_lock.
	scaleApply := func(ctx context.Context, in routes.ScaleConfigInput) (bool, error) {
		var changed bool
		err := stores.Locker.WithLock(ctx, in.TenantID, in.ProjectID, in.Environment, func(tx db.DBTX) error {
			c := &db.ScalingConfig{
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
	scaleHandler := routes.NewScaleHandler(projectStore, scaleApply, cfg.KEDAEnabled)
	scaleHandler.EnsureProductionWired() // audit M02 #14: panic-on-regression guard

	// Build list handler — lists builds for a project with optional env
	// filter PLUS exposes GET /api/v1/projects/{name}/build-envs (T2.2).
	// Plan 2026-06-08-dashboard-screens-review-fixes T2.2 + EC-2
	// SEPA-CRITICAL: NewBuildListHandlerWithGate wires the ProjectStore
	// so the /build-envs endpoint can reject cross-tenant probes with
	// 404 PROJECT_NOT_FOUND (existence-leak prevention) BEFORE the
	// underlying ListEnvsForProject query runs.
	buildListHandler := routes.NewBuildListHandlerWithGate(
		&adapters.BuildListStoreAdapter{Store: stores.BuildStore},
		projectStore,
	)

	// Project deleter — orchestrates project removal across DB, GitOps, and builds.
	// WorkflowCanceller stops running Argo Workflows during deletion (DF-2.1).
	projectDeleter := services.NewProjectDeleter(services.ProjectDeleterDeps{
		BuildStore:        stores.BuildStore,
		EnvTargetStore:    stores.EnvTargetStore,
		ReleaseStore:      stores.ReleaseStore,
		ProjectStore:      &db.PostgresProjectStore{DB: dbAdapter},
		ScalingStore:      scalingStore,
		Locker:            stores.Locker,
		GitOps:            workflows.GitOpsPusher,
		WorkflowCanceller: workflows.WorkflowSubmitter,
		Sync:              workflows.SyncClient,
	})

	// ArgoCD sync callback activator — processes webhook from ArgoCD Notifications.
	// Push-based activation: ArgoCD notifies when sync completes instead of API polling.
	syncCallbackActivator := services.NewSyncCallbackActivator(
		stores.ReleaseStore, stores.EnvTargetStore, stores.Locker, stores.EventStore, cfg.BaseDomain, logger,
	)
	// Sprint B B3 (closes code-005): wire label-based lookup so the activator
	// reads `loopzero.io/{tenant,environment}` off the live ArgoCD App CR instead
	// of LastIndex-parsing the app name (which is structurally ambiguous when
	// both tenant and env contain hyphens, e.g. `tn-loopzero-j-rollout-a`).
	// Reader uses the same mgmt dynamic client as CellPinner. Backward-compat:
	// when an App has no labels (apps pre-dating commit `8854f64e`), the
	// activator falls back to ParseArgoAppName, so existing fixtures continue
	// to work unchanged. Closure of code-005 lands progressively as old apps
	// re-render with the new label.
	if mgmtDynamic := buildMgmtDynamicClient(logger); mgmtDynamic != nil {
		syncCallbackActivator = syncCallbackActivator.WithAppLabelReader(
			services.NewDynamicArgoAppLabelReader(mgmtDynamic, "argocd"),
		)
		logger.Info("sync callback activator: label-based lookup wired (code-005 closure)")
	} else {
		logger.Warn("sync callback activator: dynamic client unavailable; falling back to LastIndex parser (code-005 hyphen ambiguity remains)")
	}
	argoCallbackHandler := routes.NewArgoCallbackHandler(syncCallbackActivator, logger, cfg.ArgoCallbackSecret)

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
		ProjectDeleter:      projectDeleter,
		ArgoCallbackHandler: argoCallbackHandler,
	}
}
