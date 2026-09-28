// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/getloopscale/loopzero/api/internal/backgroundloops"
	"github.com/getloopscale/loopzero/api/internal/builds"
	"github.com/getloopscale/loopzero/api/internal/builds/buildsadapter"
	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/domains/cfzone"
	"github.com/getloopscale/loopzero/api/internal/k8s/tenancy"
	"github.com/getloopscale/loopzero/api/internal/k8s/workload"
	"github.com/getloopscale/loopzero/api/internal/lock"
	"github.com/getloopscale/loopzero/api/internal/release"
)

// setupServices builds the stores and cluster clients the controller needs and
// returns the background loop graph from backgroundloops.Build — the same
// graph, built by the same constructors, that the API runs when it holds the
// loops lease. A client that cannot be built (workflow submitter, tenant CR
// manager) is returned as an error for main to report and exit on.
func setupServices(cfg config.Config, dbAdapter *db.PgxAdapter, logger *slog.Logger) ([]backgroundloops.Loop, error) {
	buildStore := store.NewBuildStoreSqlc(dbAdapter)
	// The event store must exist before the release store: AppendSideEffect
	// emits signal.changed events through it.
	pgEventStore := store.NewPostgresEventStore(dbAdapter, dbAdapter)
	releaseStore := store.NewReleaseStoreSqlc(dbAdapter, pgEventStore)
	envTargetStore := store.NewPostgresEnvironmentTargetStore(dbAdapter, dbAdapter)
	domainStore := &store.PostgresDomainStore{DB: dbAdapter}
	locker := &lock.PoolEnvironmentLocker{Pool: dbAdapter.Pool}

	workflowSubmitter, err := backgroundloops.NewWorkflowSubmitter(cfg, logger)
	if err != nil {
		return nil, err
	}
	syncClient, err := backgroundloops.NewSyncClient(cfg, nil, logger)
	if err != nil {
		return nil, err
	}
	gitopsPusher := backgroundloops.NewGitOpsPusher(cfg)

	tenantCRMgr, err := tenancy.NewTenantCRManager(cfg.MgmtKubeconfig, cfg.MgmtKubeContext, cfg.MaxConcurrentBuildsPerTenant)
	if err != nil {
		return nil, fmt.Errorf("tenant CR manager: %w", err)
	}
	appCRMgr := &buildsadapter.ApplicationCRAdapter{Mgr: workload.NewApplicationCRManager(tenantCRMgr.Client, "")}

	// This orchestrator's project store is DELIBERATELY left without a
	// ProjectCapChecker (M5, #106): the controller's orchestrator is used only
	// by the build-reconcile loop (OnBuildCompleted / FindReleaseForBuild /
	// RetryReleaseActivation) and never creates a project, so the per-Account
	// cap does not apply. The cap is wired on the API's orchestrator
	// (cmd/setup_orchestrator.go). If the controller ever submits builds,
	// wire SetProjectCapChecker here too.
	// The controller activates builds through the reconcile loop, so it needs
	// the same container delivery the API server wires; it used to receive it
	// implicitly from a fallback inside NewBuildOrchestrator.
	delivery := release.NewContainerDeliveryActivator(release.ContainerDeliveryDeps{
		ReleaseStore:      releaseStore,
		EnvTargetStore:    envTargetStore,
		EventStore:        pgEventStore,
		Sync:              syncClient,
		Locker:            locker,
		GitOps:            gitopsPusher,
		GitReader:         gitopsPusher,
		ArgocdSyncTimeout: cfg.ArgocdSyncTimeout,
		DeployEventStore:  store.NewDeployEventStoreSqlc(), // J-16 slice 3: deploy.succeeded outbox
	})

	orch := builds.NewBuildOrchestrator(builds.BuildOrchestratorDeps{
		BuildStore:        buildStore,
		ReleaseStore:      releaseStore,
		EnvTargetStore:    envTargetStore,
		ProjectStore:      &store.PostgresProjectStore{DB: dbAdapter},
		EventStore:        pgEventStore,
		Workflow:          workflowSubmitter,
		Sync:              syncClient,
		Locker:            locker,
		GitOps:            gitopsPusher,
		RegistryURL:       cfg.RegistryURL,
		ArgocdSyncTimeout: cfg.ArgocdSyncTimeout,
		BaseDomain:        cfg.BaseDomain,
		Delivery:          delivery,
	})

	deps := backgroundloops.Deps{
		WorkerPool:     dbAdapter,
		BuildStore:     buildStore,
		ReleaseStore:   releaseStore,
		EnvTargetStore: envTargetStore,
		EventStore:     pgEventStore,
		Locker:         locker,
		DomainStore:    domainStore,
		CellStore:      &store.PostgresCellStore{DB: dbAdapter},
		AppCR:          appCRMgr,
		Workflow:       workflowSubmitter,
		Sync:           syncClient,
		GitOps:         gitopsPusher,
		Orchestrator:   orch,
		CellID:         cellID(cfg),
	}
	// Same rule as the API: the domain provisioner uses Cloudflare zone
	// management only when it is enabled AND credentials are configured.
	if cfg.CFZoneManagement {
		deps.CFZone = cfzone.NewClient(cfg.CFAccountID, cfg.CFAPIToken)
	}
	readinessSource, err := backgroundloops.NewReadinessSource(cfg)
	if err != nil {
		return nil, err
	}
	deps.ReadinessSource = readinessSource
	return backgroundloops.Build(cfg, deps, logger), nil
}

// startBackgroundLoops launches every loop as a goroutine tracked by wg. Each
// loop runs until shutdownCtx is cancelled.
func startBackgroundLoops(shutdownCtx context.Context, wg *sync.WaitGroup, loops []backgroundloops.Loop, logger *slog.Logger) {
	wg.Add(len(loops))
	for _, loop := range loops {
		l := loop
		logger.Info("starting background loop", "loop", l.Name)
		go func() {
			defer wg.Done()
			l.Start(shutdownCtx)
		}()
	}
}
