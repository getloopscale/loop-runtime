// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/loopzero/loopzero/api/internal/adapters"
	"github.com/loopzero/loopzero/api/internal/config"
	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/loki"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/services"
)

// OrchestratorDeps holds the build orchestrator and related handlers.
type OrchestratorDeps struct {
	Orchestrator      *services.BuildOrchestrator
	BuildOrchestrator routes.BuildOrchestratorService
	LogsHandler         *routes.LogsHandler
	LogStreamHandler    *routes.LogStreamHandler
	RollbackHandler   *routes.RollbackHandler
	PromoteHandler    *routes.PromoteHandler
	StatusHandler     *routes.StatusHandler
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
) *OrchestratorDeps {
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
	statusHandler := routes.NewStatusHandler(projectStore, statusBuildStore, statusReleaseStore, cfg.BaseDomain)

	// Build list handler — lists builds for a project with optional env filter
	buildListHandler := routes.NewBuildListHandler(&adapters.BuildListStoreAdapter{Store: stores.BuildStore})

	// Project deleter — orchestrates project removal across DB, GitOps, and builds.
	// WorkflowCanceller stops running Argo Workflows during deletion (DF-2.1).
	projectDeleter := services.NewProjectDeleter(services.ProjectDeleterDeps{
		BuildStore:        stores.BuildStore,
		EnvTargetStore:    stores.EnvTargetStore,
		ProjectStore:      &db.PostgresProjectStore{DB: dbAdapter},
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
	argoCallbackHandler := routes.NewArgoCallbackHandler(syncCallbackActivator, logger, cfg.ArgoCallbackSecret)

	return &OrchestratorDeps{
		Orchestrator:        orch,
		BuildOrchestrator:   buildOrchestrator,
		LogsHandler:         logsHandler,
		LogStreamHandler:    logStreamHandler,
		RollbackHandler:     rollbackHandler,
		PromoteHandler:      promoteHandler,
		StatusHandler:       statusHandler,
		BuildListHandler:    buildListHandler,
		ProjectDeleter:      projectDeleter,
		ArgoCallbackHandler: argoCallbackHandler,
	}
}
