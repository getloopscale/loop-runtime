// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/getloopscale/loopzero/api/internal/adapters"
	"github.com/getloopscale/loopzero/api/internal/argo"
	"github.com/getloopscale/loopzero/api/internal/argocd"
	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/gitops"
	"github.com/getloopscale/loopzero/api/internal/k8s"
	"github.com/getloopscale/loopzero/api/internal/services"
	"github.com/getloopscale/loopzero/api/internal/sse"
)

// controllerDeps holds all services and stores wired during setup. Passed to
// startBackgroundLoops so the main function stays lean.
type controllerDeps struct {
	reconcileLoop          *services.BuildReconcileLoop
	buildReaper            *services.BuildReaper
	releaseReaper          *services.ReleaseReaper
	releaseConvergenceLoop *services.ReleaseConvergenceLoop
	idempotencyCleaner     *services.IdempotencyCleaner
	eventPruner            *sse.EventPruner
}

// setupServices creates all stores, clients, and the build orchestrator
// needed by the controller background loops. It exits the process on
// fatal errors (e.g. unable to create the workflow submitter).
func setupServices(cfg config.Config, dbAdapter *db.PgxAdapter, logger *slog.Logger) controllerDeps {
	// ---- Stores ----
	// T1.2b (plan 2026-05-29-sqlc-migration v1.3): sqlc-backed wrapper.
	// Satisfies db.BuildStore interface 1:1 — service field types
	// unchanged. State-machine SafeTransition + concurrent-deploy
	// rejection preserved at store layer (D4).
	buildStore := db.NewBuildStoreSqlc(dbAdapter)
	// T1.3.b of plan 2026-06-15-j18-t1.3.b-sse-pg-notify-backend v1.1:
	// EventStore must be constructed BEFORE releaseStore so AppendSideEffect
	// can emit signal.changed events via canonical SSE infrastructure.
	pgEventStore := sse.NewPostgresEventStore(dbAdapter, dbAdapter)
	// T1.1c2c (plan 2026-05-29-sqlc-migration v1.3): sqlc-backed wrapper.
	// Satisfies db.ReleaseStore interface 1:1 — service field types
	// unchanged. State-machine SafeTransition runs at store layer (D4).
	// T1.3.b v1.1: 2nd param eventStore required non-nil (EC-1).
	releaseStore := db.NewReleaseStoreSqlc(dbAdapter, pgEventStore)
	envTargetStore := db.NewPostgresEnvironmentTargetStore(dbAdapter, dbAdapter)
	domainStore := &db.PostgresDomainStore{DB: dbAdapter}

	// Environment locker (pg_advisory_xact_lock, roadmap Rule 6).
	locker := &adapters.PoolEnvironmentLocker{Pool: dbAdapter.Pool}

	// ---- Build cluster (Argo) ----
	rawWorkflowSubmitter, err := argo.NewWorkflowSubmitter(argo.WorkflowSubmitterConfig{
		Kubeconfig:          cfg.BuildKubeconfig,
		Context:             cfg.BuildKubeContext,
		RegistryURL:         cfg.RegistryURL,
		RegistryInsecure:    cfg.RegistryInsecure,
		BaseDomain:          cfg.BaseDomain,
		CosignKeyless:       cfg.CosignKeyless,       // v1.1 EC-3 wire env var
		CosignAttestEnabled: cfg.CosignAttestEnabled, // v1.1 EC-3 wire env var
		CosignFulcioURL:     cfg.CosignFulcioURL,
		CosignRekorURL:      cfg.CosignRekorURL,
		CosignInsecureSkipSCT: cfg.CosignInsecureSkipSCT, // T1.2 CR-1 fix
	})
	if err != nil {
		logger.Error("failed to create workflow submitter", "error", err)
		panic("fatal: workflow submitter creation failed") // unreachable after os.Exit
	}
	workflowSubmitter := services.NewCircuitBreakerWorkflowSubmitter(
		rawWorkflowSubmitter,
		services.CircuitBreakerConfig{
			OpenThreshold:  cfg.CBOpenThreshold,
			CloseThreshold: cfg.CBCloseThreshold,
			RetryInterval:  cfg.CBRetryInterval,
		},
		logger,
	)

	// ---- ArgoCD sync client ----
	argocdHTTPClient := &http.Client{Timeout: cfg.ArgocdClientTimeout}
	if cfg.ArgocdInsecure {
		// #nosec G402 — Insecure is opt-in, used for self-signed in dev clusters.
		argocdHTTPClient.Transport = &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		}
	}
	rawSyncClient := argocd.NewSyncClient(argocd.SyncClientConfig{
		BaseURL:    cfg.ArgocdURL,
		Token:      cfg.ArgocdToken,
		Insecure:   cfg.ArgocdInsecure,
		HTTPClient: argocdHTTPClient,
	})
	syncClient := services.NewCircuitBreakerSyncClient(rawSyncClient, services.CircuitBreakerConfig{
		OpenThreshold:  cfg.CBOpenThreshold,
		CloseThreshold: cfg.CBCloseThreshold,
		RetryInterval:  cfg.CBRetryInterval,
	}, logger)

	// ---- Gitea gitops pusher ----
	gitopsPusher := gitops.NewGiteaGitOpsPusher(gitops.GiteaGitOpsPusherConfig{
		BaseURL: cfg.GiteaURL,
		Token:   cfg.GiteaToken,
		OrgName: cfg.GiteaOrg, // B77: was hardcoded "loopzero"
	})

	// ---- k8s managers (mgmt cluster) ----
	tenantCRMgr, err := k8s.NewTenantCRManager(cfg.MgmtKubeconfig, cfg.MgmtKubeContext)
	if err != nil {
		logger.Error("failed to create tenant CR manager", "error", err)
		panic("fatal: tenant CR manager creation failed") // unreachable after os.Exit
	}
	appCRMgr := &appCRAdapter{mgr: k8s.NewApplicationCRManager(tenantCRMgr.Client, "")}

	// ---- Build orchestrator (required by reconcile loop) ----
	// M5 (#106) note: this orchestrator's project store is DELIBERATELY left
	// without a ProjectCapChecker. The controller binary's orchestrator is used
	// ONLY by the build-reconcile loop (OnBuildCompleted / FindReleaseForBuild /
	// RetryReleaseActivation) — it never calls SubmitBuild/EnsureProject, so no
	// project is ever created here and the per-Account cap does not apply. The
	// cap is wired on the API server's orchestrator (cmd/setup_orchestrator.go),
	// which owns every project-creating path. If the controller is ever changed
	// to submit/re-submit builds, wire SetProjectCapChecker here too.
	orch := services.NewBuildOrchestrator(services.BuildOrchestratorDeps{
		BuildStore:        buildStore,
		ReleaseStore:      releaseStore,
		EnvTargetStore:    envTargetStore,
		ProjectStore:      &db.PostgresProjectStore{DB: dbAdapter},
		EventStore:        pgEventStore,
		Workflow:          workflowSubmitter,
		Sync:              syncClient,
		Locker:            locker,
		GitOps:            gitopsPusher,
		RegistryURL:       cfg.RegistryURL,
		ArgocdSyncTimeout: cfg.ArgocdSyncTimeout,
		BaseDomain:        cfg.BaseDomain,
	})

	// ---- Build reconcile loop ----
	reconcileLoop := services.NewBuildReconcileLoop(
		buildStore,
		workflowSubmitter,
		orch,
		locker,
		domainStore,
		appCRMgr,
		pgEventStore,
		logger,
		services.BuildReconcileLoopConfig{
			Interval:           cfg.ReconcileInterval,
			RegistryURL:        cfg.RegistryURL,
			BaseDomain:         cfg.BaseDomain,
			ClusterIssuer:      cfg.ClusterIssuer,     // B14 (MEET-2026-04-23-058)
			PrometheusAddress:  cfg.PrometheusAddress, // argo-rollouts-progressive-delivery-plan T1.2 (D6)
			MaxConcurrent:      cfg.MaxConcurrentBuildsPerCell,
			MaxBuildsPerTenant: cfg.MaxBuildsPerTenant,
			CellID:             cellID(cfg),
		},
	)

	// ---- Build reaper ----
	buildReaper := services.NewBuildReaper(
		buildStore,
		&services.PostgresStaleBuildChecker{DB: dbAdapter},
		locker,
		workflowSubmitter,
		pgEventStore,
		logger,
		services.BuildReaperConfig{
			QueueTimeout:     cfg.BuildQueueTimeout,
			RunningTimeout:   cfg.BuildRunningTimeout,
			ReleasingTimeout: cfg.BuildReleasingTimeout,
			CancelTimeout:    cfg.BuildCancelTimeout,
			Interval:         cfg.BuildReaperInterval,
			CellID:           cellID(cfg),
		},
		reconcileLoop,
	)

	// ---- Release reaper ----
	releaseReaper := services.NewReleaseReaper(
		releaseStore,
		&services.PostgresStaleReleaseChecker{DB: dbAdapter},
		locker,
		pgEventStore,
		logger,
		services.ReleaseReaperConfig{
			ReadyTimeout:   cfg.ReleaseReadyTimeout,
			PendingTimeout: cfg.ReleasePendingTimeout,
			Interval:       cfg.ReleaseReaperInterval,
			CellID:         cellID(cfg),
		},
	)

	// ---- Release convergence loop ----
	// release-blockers-2026-06-25-set v1.1 #61: cfg.PublicDomain (canonical
	// LOOPZERO_PUBLIC_DOMAIN) preferred over BaseDomain for deploy.completed
	// URL so it matches `loopzero status`.
	releaseConvergenceLoop := services.NewReleaseConvergenceLoop(
		releaseStore,
		envTargetStore,
		syncClient,
		locker,
		pgEventStore,
		cfg.BaseDomain,
		cfg.PublicDomain,
		logger,
		services.ReleaseConvergenceLoopConfig{
			Interval:      10 * time.Second,
			SyncTimeout:   300 * time.Second,
			MaxConcurrent: 10,
			CellID:        cellID(cfg),
		},
	)

	// ---- Idempotency cleaner ----
	idempotencyCleaner := services.NewIdempotencyCleaner(
		&db.PostgresIdempotencyStore{DB: dbAdapter},
		logger,
		1*time.Hour,
	)

	// ---- SSE event pruner ----
	eventPruner := sse.NewEventPruner(
		pgEventStore,
		logger,
		24*time.Hour,
		1*time.Hour,
	)

	return controllerDeps{
		reconcileLoop:          reconcileLoop,
		buildReaper:            buildReaper,
		releaseReaper:          releaseReaper,
		releaseConvergenceLoop: releaseConvergenceLoop,
		idempotencyCleaner:     idempotencyCleaner,
		eventPruner:            eventPruner,
	}
}

// startBackgroundLoops launches all 6 background loops as goroutines tracked
// by the provided WaitGroup. Each loop runs until shutdownCtx is cancelled.
func startBackgroundLoops(
	shutdownCtx context.Context,
	wg *sync.WaitGroup,
	deps controllerDeps,
	_ config.Config,
	_ *slog.Logger,
) {
	wg.Add(6)
	go func() { defer wg.Done(); deps.reconcileLoop.Start(shutdownCtx) }()
	go func() { defer wg.Done(); deps.buildReaper.Start(shutdownCtx) }()
	go func() { defer wg.Done(); deps.releaseReaper.Start(shutdownCtx) }()
	go func() { defer wg.Done(); deps.releaseConvergenceLoop.Start(shutdownCtx) }()
	go func() { defer wg.Done(); deps.idempotencyCleaner.Start(shutdownCtx) }()
	go func() { defer wg.Done(); deps.eventPruner.Start(shutdownCtx) }()
}
