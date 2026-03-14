// Package main is the entrypoint for the LoopZero API server.
package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/loopzero/loopzero/pkg/logging"

	"github.com/loopzero/loopzero/api/internal/adapters"
	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/github"
	"github.com/loopzero/loopzero/api/internal/k8s"
	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/server"
	"github.com/loopzero/loopzero/api/internal/health"
	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/api/internal/source"
	"github.com/loopzero/loopzero/api/internal/sse"
)

func main() {
	logger := logging.NewLogger("api")

	port := os.Getenv("PORT")
	if port == "" {
		port = "3001"
	}

	// GitHub token validator
	githubValidator := &github.TokenValidator{
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}

	// Tenant CR manager (stub for Sprint 1)
	tenantCRMgr := &k8s.TenantCRManager{Logger: logger}

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
		poolCfg, err := db.NewPool(context.Background(), dbURL)
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

		// Secret store (empty placeholder — Sprint 3)
		secretStore = &db.EmptySecretStore{}

		// W1: Real source store — MinIO (S3-compatible)
		minioEndpoint := os.Getenv("MINIO_ENDPOINT")
		if minioEndpoint == "" {
			minioEndpoint = "minio.loopzero-system.svc.cluster.local:9000"
		}
		minioAccessKey := os.Getenv("MINIO_ACCESS_KEY")
		if minioAccessKey == "" {
			minioAccessKey = "loopzero-admin"
		}
		minioSecretKey := os.Getenv("MINIO_SECRET_KEY")
		if minioSecretKey == "" {
			minioSecretKey = "loopzero-dev-password"
		}
		minioStore, err := source.NewMinIOSourceStore(source.MinIOConfig{
			Endpoint:  minioEndpoint,
			AccessKey: minioAccessKey,
			SecretKey: minioSecretKey,
			Bucket:    "loopzero-artifacts",
			UseSSL:    false,
		})
		if err != nil {
			logger.Error("failed to create MinIO source store", "error", err)
			os.Exit(1)
		}
		sourceStore = minioStore

		// Environment locker using real pgxpool.Pool
		locker := &adapters.PoolEnvironmentLocker{Pool: dbAdapter.Pool}
		// TODO(W2): workflowSubmitter = &argo.ArgoWorkflowSubmitter{...}
		// TODO(W3): gitOpsPusher = &gitea.GitOpsPusher{...}
		// TODO(W4): syncClient = &argocd.SyncClient{...}
		// W5: Real health prober — HTTP GET with retries
		healthProber := &health.HTTPProber{
			Client: &http.Client{Timeout: 10 * time.Second},
		}

		// TODO(W1): sourceStore = &source.GiteaSourceStore{...}
		// TODO(W2): workflowSubmitter = &argo.ArgoWorkflowSubmitter{...}
		// TODO(W3): gitOpsPusher = &gitea.GitOpsPusher{...}
		// TODO(W4): syncClient = &argocd.SyncClient{...}

		var workflowSubmitter services.WorkflowSubmitter   // nil — must implement
		var gitOpsPusher services.GitOpsPusher             // nil — must implement
		var syncClient services.SyncClient                  // nil — must implement
		_ = workflowSubmitter
		_ = gitOpsPusher
		_ = syncClient

		orch := services.NewBuildOrchestrator(services.BuildOrchestratorDeps{
			BuildStore:     buildStore,
			ReleaseStore:   releaseStore,
			EnvTargetStore: envTargetStore,
			EventStore:     pgEventStore,
			Workflow:       workflowSubmitter,
			GitOps:         gitOpsPusher,
			Health:         healthProber,
			Sync:           syncClient,
			Locker:         locker,
		})

		// Route-level adapter
		buildOrchestrator = &adapters.BuildOrchestratorAdapter{
			Orch:       orch,
			BuildStore: buildStore,
		}

		// SSE stream reader adapter
		buildStoreReader = &adapters.BuildStoreReaderAdapter{Store: buildStore}

		// Argo callback secret
		argoCallbackSecret = os.Getenv("LOOPZERO_ARGO_CALLBACK_SECRET")

		logger.Info("sprint 2 dependencies wired — real implementations required")
	} else {
		logger.Warn("DATABASE_URL not set — running without database")
	}

	cfg := server.Config{
		Port:             port,
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
	}

	if err := server.Run(cfg); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
