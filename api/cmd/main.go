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

	// In production, fail fast if security-sensitive env vars are missing.
	// In dev mode, fallback defaults are acceptable.
	if os.Getenv("LOOPZERO_ENV") == "production" {
		required := map[string]string{
			"DATABASE_URL":              os.Getenv("DATABASE_URL"),
			"LOOPZERO_ARGO_CALLBACK_SECRET": os.Getenv("LOOPZERO_ARGO_CALLBACK_SECRET"),
			"GITEA_TOKEN":              os.Getenv("GITEA_TOKEN"),
			"ARGOCD_TOKEN":             os.Getenv("ARGOCD_TOKEN"),
		}
		if err := server.ValidateProductionEnv(logger, required); err != nil {
			os.Exit(1)
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "3001"
	}

	// GitHub token validator
	githubValidator := &github.TokenValidator{
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}

	// Tenant CR manager — real Kubernetes client for Tenant CRs.
	// Uses in-cluster config when running inside the mgmt cluster (Tilt/production),
	// falls back to kubeconfig for local development outside the cluster.
	mgmtKubeconfig := os.Getenv("MGMT_KUBECONFIG")
	if mgmtKubeconfig == "" {
		home, _ := os.UserHomeDir()
		mgmtKubeconfig = home + "/.kube/config"
	}
	mgmtContext := os.Getenv("MGMT_KUBE_CONTEXT")
	if mgmtContext == "" {
		mgmtContext = "k3d-loopzero-mgmt"
	}
	tenantCRMgr, err := k8s.NewTenantCRManager(mgmtKubeconfig, mgmtContext)
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

		// Secret store — queries PostgreSQL for secret metadata (never values)
		secretStore = &db.PostgresSecretStore{DB: dbAdapter}

		// W1: Real source store — Gitea source repo
		// Source is pushed to Gitea (loopzero-source-{tenant}-{project}) per spec §7.1.
		// The Argo Workflow clones from this Gitea repo (step 13a).
		giteaURL := os.Getenv("GITEA_URL")
		if giteaURL == "" {
			giteaURL = "http://gitea-http.loopzero-system.svc.cluster.local:3000"
		}
		giteaToken := os.Getenv("GITEA_TOKEN")

		giteaSourceStore, err := source.NewGiteaSourceStore(source.GiteaSourceStoreConfig{
			BaseURL: giteaURL,
			Token:   giteaToken,
			OrgName: "loopzero",
		})
		if err != nil {
			logger.Error("failed to create Gitea source store", "error", err)
			os.Exit(1)
		}
		sourceStore = giteaSourceStore

		// Environment locker using real pgxpool.Pool
		locker := &adapters.PoolEnvironmentLocker{Pool: dbAdapter.Pool}

		// W5: Real health prober — HTTP GET with retries
		// When WORKLOAD_INGRESS_URL is set, probes go through the Ingress controller
		// with Host header override. This allows health probing without DNS resolution —
		// essential for local dev (k3d) and any environment where *.loopzero.local doesn't resolve.
		workloadIngressURL := os.Getenv("WORKLOAD_INGRESS_URL")
		healthProber := &health.HTTPProber{
			Client:     &http.Client{Timeout: 10 * time.Second},
			IngressURL: workloadIngressURL,
		}

		// W2: Real workflow submitter — Argo Workflows in build cluster
		buildKubeconfig := os.Getenv("BUILD_KUBECONFIG")
		if buildKubeconfig == "" {
			home, _ := os.UserHomeDir()
			buildKubeconfig = home + "/.kube/config"
		}
		buildContext := os.Getenv("BUILD_KUBE_CONTEXT")
		if buildContext == "" {
			buildContext = "k3d-loopzero-build"
		}
		callbackSecret := os.Getenv("LOOPZERO_ARGO_CALLBACK_SECRET")
		if callbackSecret == "" {
			callbackSecret = "loopzero-dev-callback-secret"
		}
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
		registryURL := os.Getenv("REGISTRY_URL")
		if registryURL == "" {
			registryURL = "k3d-loopzero-registry:5000"
		}

		// Cross-cluster URLs for workflow parameters (build cluster → mgmt services)
		giteaCrossClusterURL := os.Getenv("GITEA_CROSS_CLUSTER_URL")
		if giteaCrossClusterURL == "" {
			giteaCrossClusterURL = giteaURL // fallback to internal URL
		}
		apiCrossClusterURL := os.Getenv("API_CROSS_CLUSTER_URL")
		if apiCrossClusterURL == "" {
			apiCrossClusterURL = "http://loopzero-api.loopzero-system.svc.cluster.local:3001"
		}

		workflowSubmitter, err := argo.NewWorkflowSubmitter(argo.WorkflowSubmitterConfig{
			Kubeconfig:     buildKubeconfig,
			Context:        buildContext,
			Namespace:      "argo",
			TemplateName:   "loopzero-build-nodejs-v1",
			CallbackSecret: callbackSecret,
			GiteaURL:       giteaCrossClusterURL,
			GiteaToken:     giteaToken,
			MinIOEndpoint:  minioEndpoint,
			MinIOAccessKey: minioAccessKey,
			MinIOSecretKey: minioSecretKey,
			RegistryURL:    registryURL,
			CallbackURL:    apiCrossClusterURL + "/api/v1/builds",
		})
		if err != nil {
			logger.Error("failed to create workflow submitter", "error", err)
			os.Exit(1)
		}

		// W4: Real ArgoCD sync client
		argocdURL := os.Getenv("ARGOCD_URL")
		if argocdURL == "" {
			argocdURL = "http://argocd-server.argocd.svc.cluster.local:80"
		}
		argocdToken := os.Getenv("ARGOCD_TOKEN")

		rawSyncClient := argocd.NewSyncClient(argocd.SyncClientConfig{
			BaseURL:  argocdURL,
			Token:    argocdToken,
			Insecure: os.Getenv("ARGOCD_INSECURE") == "true",
		})
		// PG-backed circuit breaker — shared state across all API replicas.
		// Replaces in-memory circuit breaker (DA-7 tech debt resolved).
		cbStore := db.NewCircuitBreakerStore(dbAdapter)
		syncClient := services.NewPGCircuitBreakerSyncClient(rawSyncClient, cbStore, services.PGCircuitBreakerConfig{
			Key:            "argocd",
			OpenThreshold:  3,
			CloseThreshold: 2,
			RetryInterval:  30 * time.Second,
		})

		// Step 6: API owns GitOps push — build pipeline only produces artifacts.
		// The GitOpsPusher commits manifests to the tenant GitOps repo in Gitea
		// after a build succeeds, enabling retry-without-rebuild.
		gitopsPusher := gitops.NewGiteaGitOpsPusher(gitops.GiteaGitOpsPusherConfig{
			BaseURL: giteaURL,
			Token:   giteaToken,
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

		// Route-level adapter
		buildOrchestrator = &adapters.BuildOrchestratorAdapter{
			Orch:       orch,
			BuildStore: buildStore,
		}

		// SSE stream reader adapter
		buildStoreReader = &adapters.BuildStoreReaderAdapter{Store: buildStore}

		// Argo callback secret
		argoCallbackSecret = os.Getenv("LOOPZERO_ARGO_CALLBACK_SECRET")

		// Startup reconciler — recovers orphaned builds on boot (spec §12.1).
		// Runs ONCE before the server starts accepting requests.
		reconciler := services.NewStartupReconciler(buildStore, workflowSubmitter, locker, logger)
		reconciler.Reconcile(context.Background())

		// Build reaper — enforces spec §5.1 timeouts (QUEUED 5min, RUNNING 30min, CANCELLING 60s)
		reaper := services.NewBuildReaper(
			buildStore,
			&services.PostgresStaleBuildChecker{DB: dbAdapter},
			locker,
			logger,
			services.DefaultBuildReaperConfig(),
		)
		go reaper.Start(context.Background())

		logger.Info("sprint 2 dependencies wired — real implementations")
	} else {
		logger.Warn("DATABASE_URL not set — running without database")
	}

	metricsPort := os.Getenv("METRICS_PORT")
	if metricsPort == "" {
		metricsPort = "9090"
	}

	cfg := server.Config{
		Port:             port,
		MetricsPort:      metricsPort,
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
