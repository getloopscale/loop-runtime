// Package main is the entrypoint for the LoopZero API server.
package main

import (
	"context"
	"net/http"
	"os"

	"github.com/loopzero/loopzero/pkg/logging"

	"github.com/loopzero/loopzero/api/internal/adapters"
	"github.com/loopzero/loopzero/api/internal/config"
	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/github"
	"github.com/loopzero/loopzero/api/internal/k8s"
	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/server"
	"github.com/loopzero/loopzero/api/internal/argo"
	"github.com/loopzero/loopzero/api/internal/argocd"
	"github.com/loopzero/loopzero/api/internal/gitops"
	"github.com/loopzero/loopzero/api/internal/health"
	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/api/internal/source"
	"github.com/loopzero/loopzero/api/internal/sse"
)

func main() {
	logger := logging.NewLogger("api")
	cfg := config.Load()

	// In production, fail fast if security-sensitive env vars are missing.
	if cfg.IsProduction() {
		required := map[string]string{
			"DATABASE_URL":              os.Getenv("DATABASE_URL"),
			"LOOPZERO_ARGO_CALLBACK_SECRET": os.Getenv("LOOPZERO_ARGO_CALLBACK_SECRET"),
			"GITEA_TOKEN":               cfg.GiteaToken,
			"ARGOCD_TOKEN":              cfg.ArgocdToken,
		}
		if err := server.ValidateProductionEnv(logger, required); err != nil {
			os.Exit(1)
		}
	}

	// GitHub token validator
	githubValidator := &github.TokenValidator{
		HTTPClient: &http.Client{Timeout: cfg.HTTPClientTimeout},
	}

	// Tenant CR manager — real Kubernetes client for Tenant CRs.
	tenantCRMgr, err := k8s.NewTenantCRManager(cfg.MgmtKubeconfig, cfg.MgmtKubeContext)
	if err != nil {
		logger.Error("failed to create tenant CR manager", "error", err)
		os.Exit(1)
	}

	// Database connection (optional — graceful degradation if not configured)
	var memberStore middleware.MembershipStore
	var projectStore routes.ProjectStore
	var dbHealth routes.DBHealthChecker
	var dbAdapter *db.PgxAdapter

	// Sprint 2 dependencies (nil when DATABASE_URL is not set)
	var sourceStore source.SourceStore
	var buildOrchestrator routes.BuildOrchestratorService
	var argoCallbackSecret string
	var eventStore sse.EventStore
	var buildStoreReader sse.BuildStoreReader
	var secretStore routes.SecretStore

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL != "" {
		poolCfg, err := db.NewPool(context.Background(), dbURL, db.PoolConfig{
			MaxConns:         cfg.DBMaxConns,
			MinConns:         cfg.DBMinConns,
			MaxConnIdleTime:  cfg.DBMaxConnIdleTime,
			MaxConnLifetime:  cfg.DBMaxConnLifetime,
			HealthCheckPeriod: cfg.DBHealthCheckTime,
			ConnectTimeout:   cfg.DBConnectTimeout,
			StatementTimeout: cfg.DBStatementTimeout,
		})
		if err != nil {
			logger.Error("failed to connect to database", "error", err)
			os.Exit(1)
		}
		dbAdapter = poolCfg
		defer dbAdapter.Close()

		// Sprint 1 stores
		memberStore = &db.PostgresMembershipStore{DB: dbAdapter}
		projectStore = &db.PostgresProjectStore{DB: dbAdapter}
		dbHealth = dbAdapter

		// Sprint 2 stores
		buildStore := db.NewPostgresBuildStore(dbAdapter)
		releaseStore := db.NewPostgresReleaseStore(dbAdapter)
		envTargetStore := db.NewPostgresEnvironmentTargetStore(dbAdapter, dbAdapter)
		pgEventStore := sse.NewPostgresEventStore(dbAdapter, dbAdapter)
		eventStore = pgEventStore

		// Secret store
		secretStore = &db.PostgresSecretStore{DB: dbAdapter}

		// Source store — Gitea source repo
		giteaSourceStore, err := source.NewGiteaSourceStore(source.GiteaSourceStoreConfig{
			BaseURL: cfg.GiteaURL,
			Token:   cfg.GiteaToken,
			OrgName: "loopzero",
		})
		if err != nil {
			logger.Error("failed to create Gitea source store", "error", err)
			os.Exit(1)
		}
		sourceStore = giteaSourceStore

		// Environment locker using real pgxpool.Pool
		locker := &adapters.PoolEnvironmentLocker{Pool: dbAdapter.Pool}

		// Health prober — HTTP GET with retries
		healthProber := &health.HTTPProber{
			Client:     &http.Client{Timeout: cfg.HTTPClientTimeout},
			IngressURL: cfg.WorkloadIngressURL,
		}

		// Workflow submitter — Argo Workflows in build cluster
		workflowSubmitter, err := argo.NewWorkflowSubmitter(argo.WorkflowSubmitterConfig{
			Kubeconfig:     cfg.BuildKubeconfig,
			Context:        cfg.BuildKubeContext,
			Namespace:      cfg.ArgoNamespace,
			TemplateName:   cfg.WorkflowTemplate,
			CallbackSecret: cfg.ArgoCallbackSecret,
			GiteaURL:       cfg.GiteaCrossCluster(),
			GiteaToken:     cfg.GiteaToken,
			MinIOEndpoint:  cfg.MinIOEndpoint,
			MinIOAccessKey: cfg.MinIOAccessKey,
			MinIOSecretKey: cfg.MinIOSecretKey,
			RegistryURL:    cfg.RegistryURL,
			CallbackURL:    cfg.APICrossClusterURL + "/api/v1/builds",
		})
		if err != nil {
			logger.Error("failed to create workflow submitter", "error", err)
			os.Exit(1)
		}

		// ArgoCD sync client with PG-backed circuit breaker
		rawSyncClient := argocd.NewSyncClient(argocd.SyncClientConfig{
			BaseURL:  cfg.ArgocdURL,
			Token:    cfg.ArgocdToken,
			Insecure: cfg.ArgocdInsecure,
		})
		cbStore := db.NewCircuitBreakerStore(dbAdapter)
		syncClient := services.NewPGCircuitBreakerSyncClient(rawSyncClient, cbStore, services.PGCircuitBreakerConfig{
			Key:            cfg.CBKey,
			OpenThreshold:  cfg.CBOpenThreshold,
			CloseThreshold: cfg.CBCloseThreshold,
			RetryInterval:  cfg.CBRetryInterval,
		})

		// GitOps pusher — commits manifests to Gitea after build succeeds
		gitopsPusher := gitops.NewGiteaGitOpsPusher(gitops.GiteaGitOpsPusherConfig{
			BaseURL: cfg.GiteaURL,
			Token:   cfg.GiteaToken,
			OrgName: "loopzero",
		})

		orch := services.NewBuildOrchestrator(services.BuildOrchestratorDeps{
			BuildStore:     buildStore,
			ReleaseStore:   releaseStore,
			EnvTargetStore: envTargetStore,
			EventStore:     pgEventStore,
			Workflow:       workflowSubmitter,
			Health:         healthProber,
			Sync:           syncClient,
			Locker:         locker,
			GitOps:         gitopsPusher,
		})

		buildOrchestrator = &adapters.BuildOrchestratorAdapter{
			Orch:       orch,
			BuildStore: buildStore,
		}

		buildStoreReader = &adapters.BuildStoreReaderAdapter{Store: buildStore}
		argoCallbackSecret = cfg.ArgoCallbackSecret

		// Startup reconciler — recovers orphaned builds on boot (spec §12.1)
		reconciler := services.NewStartupReconciler(buildStore, workflowSubmitter, locker, logger)
		reconciler.Reconcile(context.Background())

		// Build reaper — enforces spec §5.1 timeouts
		reaper := services.NewBuildReaper(
			buildStore,
			&services.PostgresStaleBuildChecker{DB: dbAdapter},
			locker,
			logger,
			services.BuildReaperConfig{
				QueueTimeout:   cfg.BuildQueueTimeout,
				RunningTimeout: cfg.BuildRunningTimeout,
				CancelTimeout:  cfg.BuildCancelTimeout,
				Interval:       cfg.BuildReaperInterval,
			},
		)
		go reaper.Start(context.Background())

		logger.Info("sprint 2 dependencies wired — real implementations")
	} else {
		logger.Warn("DATABASE_URL not set — running without database")
	}

	srvCfg := server.Config{
		Port:             cfg.Port,
		MetricsPort:      cfg.MetricsPort,
		Logger:           logger,
		DB:               dbHealth,
		TokenValidator:   githubValidator,
		MemberStore:      memberStore,
		ProjectStore:     projectStore,
		TenantCRMgr:      tenantCRMgr,
		IdempotencyStore: func() middleware.IdempotencyStore {
			if dbAdapter != nil {
				return &db.PostgresIdempotencyStore{DB: dbAdapter}
			}
			return nil
		}(),
		SourceStore:        sourceStore,
		BuildOrchestrator:  buildOrchestrator,
		ArgoCallbackSecret: argoCallbackSecret,
		EventStore:         eventStore,
		BuildStoreReader:   buildStoreReader,
		SecretStore:        secretStore,
		ReadHeaderTimeout:  cfg.ReadHeaderTimeout,
		ReadTimeout:        cfg.ReadTimeout,
		IdleTimeout:        cfg.IdleTimeout,
		ShutdownTimeout:    cfg.GracefulShutdownTime,
	}

	if err := server.Run(srvCfg); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
