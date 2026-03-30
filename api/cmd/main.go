// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package main is the entrypoint for the LoopZero API server.
package main

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

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
	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/api/internal/loki"
	"github.com/loopzero/loopzero/api/internal/cfkv"
	s3uploader "github.com/loopzero/loopzero/api/internal/s3"
	"github.com/loopzero/loopzero/api/internal/source"
	"github.com/loopzero/loopzero/api/internal/sse"

	"k8s.io/klog/v2"
)

func main() {
	// Silence klog (k8s.io/client-go) so it doesn't emit plain text logs
	// that break our structured JSON output. Kubernetes client libraries
	// use klog internally; without this, their logs bypass slog and appear
	// as unstructured plain text on stderr.
	klogFlags := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(klogFlags)
	_ = klogFlags.Set("logtostderr", "false")
	_ = klogFlags.Set("stderrthreshold", "FATAL")

	logger := logging.NewLogger("api")
	// Set the default slog logger so that all slog.Error/Warn/Info calls
	// (used in services, middleware, etc.) produce structured JSON output
	// matching our configured handler instead of plain text.
	slog.SetDefault(logger)

	cfg := config.Load()
	// SEC-2: Validate() now returns error for fatal security violations (e.g., ARGOCD_INSECURE=true in production).
	if err := cfg.Validate(); err != nil {
		logger.Error("configuration validation failed", "error", err)
		os.Exit(1)
	}

	// A-05: Create the shutdown context BEFORE starting any background goroutines.
	// On SIGTERM/SIGINT, server.Run() calls shutdownCancel, which signals all
	// goroutines to stop. Only after they exit does the connection pool close.
	// Shutdown order: signal → cancel context → goroutines stop → server drains → pool closes.
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())

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

	// Shared HTTP transport with connection pooling (I-03).
	// All HTTP clients (GitHub, ArgoCD, health prober) share this transport
	// so idle connections are reused across components.
	httpTransport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
	httpClient := &http.Client{
		Timeout:   cfg.HTTPClientTimeout,
		Transport: httpTransport,
	}

	// GitHub token validator
	githubValidator := &github.TokenValidator{
		HTTPClient: httpClient,
	}

	// Tenant CR manager — real Kubernetes client for Tenant CRs.
	tenantCRMgr, err := k8s.NewTenantCRManager(cfg.MgmtKubeconfig, cfg.MgmtKubeContext)
	if err != nil {
		logger.Error("failed to create tenant CR manager", "error", err)
		os.Exit(1)
	}

	// Secret K8s manager — creates K8s Secrets on mgmt + ExternalSecret CRs on workload for ESO sync
	secretK8sMgr, err := k8s.NewSecretK8sManager(
		cfg.MgmtKubeconfig, cfg.MgmtKubeContext,
		cfg.RuntimeKubeconfig, cfg.RuntimeKubeContext,
	)
	if err != nil {
		logger.Warn("secret K8s manager not available — ESO sync disabled", "error", err)
	}

	// Database connection (optional — graceful degradation if not configured)
	var memberStore middleware.MembershipStore
	var projectStore routes.ProjectStore
	var dbHealth routes.DBHealthChecker
	var dbAdapter *db.PgxAdapter

	// Sprint 2 dependencies (nil when DATABASE_URL is not set)
	var sourceStore source.SourceStore
	var objUploader routes.ObjectUploader
	var buildOrchestrator routes.BuildOrchestratorService
	var eventStore sse.EventStore
	var buildStoreReader sse.BuildStoreReader
	var secretStore routes.SecretStore
	var bootstrapStore routes.BootstrapStore
	var logsHandler *routes.LogsHandler
	var argoCallbackHandler *routes.ArgoCallbackHandler
	var rollbackHandler *routes.RollbackHandler
	var statusHandler *routes.StatusHandler
	var projectDeleter routes.ProjectDeleter

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

		// Secret store — SEC-001: encryption at rest when key is configured.
		var encKey []byte
		if cfg.SecretEncryptionKey != "" {
			var err error
			encKey, err = hex.DecodeString(cfg.SecretEncryptionKey)
			if err != nil {
				logger.Error("invalid LOOPZERO_SECRET_ENCRYPTION_KEY (must be 64 hex chars for AES-256)", "error", err)
				os.Exit(1)
			}
		}
		secretStoreImpl, err := db.NewPostgresSecretStore(dbAdapter, encKey)
		if err != nil {
			logger.Error("failed to create secret store", "error", err)
			os.Exit(1)
		}
		secretStore = secretStoreImpl

		// Bootstrap store — internal-only endpoint for platform bootstrap
		bootstrapStore = &db.PostgresBootstrapStore{DB: dbAdapter}

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

		// S3 uploader for source bundles — build cluster reads from Spaces.
		// Source is dual-written: Gitea (git history) + Spaces (build access).
		if cfg.MinIOEndpoint != "" && cfg.MinIOAccessKey != "" && cfg.MinIOSecretKey != "" {
			objUploader = s3uploader.New(cfg.MinIOEndpoint, cfg.MinIOAccessKey, cfg.MinIOSecretKey)
			logger.Info("s3 uploader enabled", "endpoint", cfg.MinIOEndpoint)
		}

		// Environment locker using real pgxpool.Pool
		locker := &adapters.PoolEnvironmentLocker{Pool: dbAdapter.Pool}

		// Workflow submitter — Argo Workflows in build cluster
		// C-01 fix: CallbackURL/CallbackSecret removed from workflow config.
		// The workflow no longer calls the API. Build completion is detected
		// by the BuildReconcileLoop polling Argo Workflow CRDs.
		rawWorkflowSubmitter, err := argo.NewWorkflowSubmitter(argo.WorkflowSubmitterConfig{
			Kubeconfig:       cfg.BuildKubeconfig,
			Context:          cfg.BuildKubeContext,
			RegistryURL:      cfg.RegistryURL,
			RegistryInsecure: cfg.RegistryInsecure,
			BaseDomain:       cfg.BaseDomain,
		})
		if err != nil {
			logger.Error("failed to create workflow submitter", "error", err)
			os.Exit(1)
		}

		// REVIEW-FIX: Wire CircuitBreakerWorkflowSubmitter to protect against
		// Argo Workflow API outages. Without this, 10 concurrent deploys against
		// a down Argo controller waste ~70s in retries instead of failing fast.
		workflowSubmitter := services.NewCircuitBreakerWorkflowSubmitter(
			rawWorkflowSubmitter,
			services.CircuitBreakerConfig{
				OpenThreshold:  cfg.CBOpenThreshold,
				CloseThreshold: cfg.CBCloseThreshold,
				RetryInterval:  cfg.CBRetryInterval,
			},
			logger,
		)

		// ArgoCD HTTP client — shares transport pooling but uses ArgoCD-specific timeout.
		// When Insecure is true, we need a separate transport for TLS skip.
		argocdHTTPClient := &http.Client{Timeout: cfg.ArgocdClientTimeout}
		if cfg.ArgocdInsecure {
			argocdHTTPClient.Transport = &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			}
		} else {
			argocdHTTPClient.Transport = httpTransport
		}

		// ArgoCD sync client with in-memory circuit breaker (M-02 fix).
		// State is per-replica, not shared via PostgreSQL. This eliminates
		// write amplification and the circular dependency where the circuit
		// breaker protecting ArgoCD depended on PostgreSQL being healthy.
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

		// GitOps pusher — commits manifests to Gitea after build succeeds
		gitopsPusher := gitops.NewGiteaGitOpsPusher(gitops.GiteaGitOpsPusherConfig{
			BaseURL: cfg.GiteaURL,
			Token:   cfg.GiteaToken,
			OrgName: "loopzero",
		})

		// Cloudflare KV client for static frontend routing (Sprint 10).
		// nil when CF credentials not configured (local dev).
		// IMPORTANT: Assign to the interface type directly to avoid the Go nil interface trap.
		// A typed nil *cfkv.Client assigned to a StaticRoutingClient interface is NOT nil,
		// which causes panics when methods are called on it.
		var kvClient services.StaticRoutingClient
		if c := cfkv.NewClient(cfg.CFAccountID, cfg.CFKVNamespaceID, cfg.CFAPIToken); c != nil {
			kvClient = c
			slog.Info("cloudflare KV client configured for static frontend routing")
		}

		orch := services.NewBuildOrchestrator(services.BuildOrchestratorDeps{
			BuildStore:        buildStore,
			ReleaseStore:      releaseStore,
			EnvTargetStore:    envTargetStore,
			EventStore:        pgEventStore,
			Workflow:          workflowSubmitter,
			Sync:              syncClient,
			Locker:            locker,
			GitOps:            gitopsPusher,
			RegistryURL:       cfg.RegistryURL,
			ArgocdSyncTimeout: cfg.ArgocdSyncTimeout,
			KVClient:          kvClient,
			BaseDomain:        cfg.BaseDomain,
		})

		buildOrchestrator = &adapters.BuildOrchestratorAdapter{
			Orch:       orch,
			BuildStore: buildStore,
		}

		buildStoreReader = &adapters.BuildStoreReaderAdapter{Store: buildStore}

		// Build logs via Loki
		lokiClient := loki.NewClient(cfg.LokiURL, httpClient, logger)
		logBuildStore := &adapters.LogBuildStoreAdapter{Store: buildStore}
		logsHandler = routes.NewLogsHandler(logBuildStore, lokiClient)

		// Rollback orchestrator — coordinates release rollback with lock + state machine
		rollbackOrch := services.NewRollbackOrchestrator(services.RollbackOrchestratorDeps{
			Releases:   releaseStore,
			EnvTargets: envTargetStore,
			GitReader:  gitopsPusher,
			GitPusher:  gitopsPusher,
			Sync:       syncClient,
			Locker:     locker,
			Logger:     logger,
		})
		rollbackService := &adapters.RollbackServiceAdapter{Orchestrator: rollbackOrch}
		rollbackHandler = routes.NewRollbackHandler(projectStore, rollbackService, cfg.BaseDomain)

		// Status handler — wires project, build, and release stores (H6 status endpoint)
		statusBuildStore := &adapters.StatusBuildStoreAdapter{Store: buildStore}
		statusReleaseStore := &adapters.StatusReleaseStoreAdapter{Store: releaseStore}
		statusHandler = routes.NewStatusHandler(projectStore, statusBuildStore, statusReleaseStore, cfg.BaseDomain)

		// Project deleter — orchestrates project removal across DB, GitOps, and builds.
		// WorkflowCanceller stops running Argo Workflows during deletion (DF-2.1).
		projectDeleter = services.NewProjectDeleter(services.ProjectDeleterDeps{
			BuildStore:        buildStore,
			EnvTargetStore:    envTargetStore,
			ProjectStore:      &db.PostgresProjectStore{DB: dbAdapter},
			Locker:            locker,
			GitOps:            gitopsPusher,
			WorkflowCanceller: workflowSubmitter,
			Sync:              syncClient,
		})

		// C-01/C-03 fix: BuildReconcileLoop replaces both the HTTP callback
		// and the StartupReconciler. The control plane POLLS the build cluster's
		// Workflow CRDs instead of the build cluster calling the API.
		reconcileLoop := services.NewBuildReconcileLoop(
			buildStore,
			workflowSubmitter,
			orch,
			locker,
			pgEventStore,
			logger,
			services.BuildReconcileLoopConfig{
				Interval:    cfg.ReconcileInterval,
				RegistryURL: cfg.RegistryURL,
				BaseDomain:  cfg.BaseDomain,
			},
		)
		go reconcileLoop.Start(shutdownCtx)

		// Build reaper — enforces spec §5.1 timeouts
		// MOD-6: reconcileLoop is passed so reaper skips builds being reconciled.
		reaper := services.NewBuildReaper(
			buildStore,
			&services.PostgresStaleBuildChecker{DB: dbAdapter},
			locker,
			workflowSubmitter,
			eventStore,
			logger,
			services.BuildReaperConfig{
				QueueTimeout:   cfg.BuildQueueTimeout,
				RunningTimeout: cfg.BuildRunningTimeout,
				CancelTimeout:  cfg.BuildCancelTimeout,
				Interval:       cfg.BuildReaperInterval,
			},
			reconcileLoop,
		)
		go reaper.Start(shutdownCtx)

		// Release reaper — enforces spec §5.2 timeouts
		// Releases stuck in READY past 5min → GIT_MIRROR_FAILED
		// Releases stuck in PENDING past 2min → SYNC_FAILED
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
			},
		)
		go releaseReaper.Start(shutdownCtx)

		// MEET-2026-03-27-012 Phase 1.1: Release Convergence Loop
		// Asynchronously verifies PENDING releases converge (ArgoCD sync + health).
		// Decouples Phase D from the synchronous build reconcile loop.
		releaseConvergenceLoop := services.NewReleaseConvergenceLoop(
			releaseStore,
			envTargetStore,
			syncClient,
			locker,
			eventStore,
			logger,
			services.ReleaseConvergenceLoopConfig{
				Interval: 10 * time.Second,
				// SyncTimeout must exceed: gitops push (~5s) + ArgoCD sync trigger (~2s)
				// + cert-manager DNS-01 challenge (~90s) + ArgoCD health check (~30s).
				// First deploy is slowest due to TLS certificate issuance.
				SyncTimeout:   300 * time.Second,
				MaxConcurrent: 10,
			},
		)
		go releaseConvergenceLoop.Start(shutdownCtx)

		// Idempotency cleaner — prunes expired keys every hour (A-08)
		idempotencyCleaner := services.NewIdempotencyCleaner(
			&db.PostgresIdempotencyStore{DB: dbAdapter},
			logger,
			1*time.Hour,
		)
		go idempotencyCleaner.Start(shutdownCtx)

		// SSE event pruner — removes events older than 24h every hour (C-06)
		eventPruner := sse.NewEventPruner(
			pgEventStore,
			logger,
			24*time.Hour,
			1*time.Hour,
		)
		go eventPruner.Start(shutdownCtx)

		// ArgoCD sync callback activator — processes webhook from ArgoCD Notifications.
		// Push-based activation: ArgoCD notifies when sync completes instead of API polling.
		syncCallbackActivator := services.NewSyncCallbackActivator(
			releaseStore, envTargetStore, locker, eventStore, logger,
		)
		argoCallbackHandler = routes.NewArgoCallbackHandler(syncCallbackActivator, logger, cfg.ArgoCallbackSecret)

		logger.Info("sprint 2 dependencies wired — real implementations")
	} else {
		logger.Warn("DATABASE_URL not set — running without database")
	}

	// Rate limiter: 60 requests per minute per tenant (C-01).
	// Generous for normal use but prevents abuse.
	rateLimiter := middleware.NewRateLimiter(60, 1*time.Minute)

	// C-01 fix: BuildHandler no longer handles callbacks. The BuildReconcileLoop
	// detects build completion by polling Argo Workflow CRDs.
	var buildHandler *routes.BuildHandler
	if buildOrchestrator != nil {
		buildHandler = routes.NewBuildHandler(buildOrchestrator, logger)
	}

	// MEET-2026-03-27-012 Phase 1.3: Deep health checks for all critical dependencies.
	// Each checker probes a service endpoint. If any is unreachable, /api/v1/health/deep returns 503.
	var healthServices []routes.ServiceHealthChecker
	if cfg.ArgocdURL != "" {
		healthServices = append(healthServices, routes.NewHTTPServiceChecker(
			"argocd", cfg.ArgocdURL+"/api/v1/applications", cfg.ArgocdToken, nil))
	}
	if cfg.GiteaURL != "" {
		healthServices = append(healthServices, routes.NewHTTPServiceChecker(
			"gitea", cfg.GiteaURL+"/api/v1/repos/search?limit=1", cfg.GiteaToken, nil))
	}

	srvCfg := server.Config{
		Port:             cfg.Port,
		MetricsPort:      cfg.MetricsPort,
		Logger:           logger,
		DB:               dbHealth,
		HealthServices:   healthServices,
		TokenValidator:   githubValidator,
		MemberStore:      memberStore,
		ProjectStore:     projectStore,
		TenantCRMgr:      tenantCRMgr,
		ProjectDeleter:   projectDeleter,
		IdempotencyStore: func() middleware.IdempotencyStore {
			if dbAdapter != nil {
				return &db.PostgresIdempotencyStore{DB: dbAdapter}
			}
			return nil
		}(),
		SourceStore:        sourceStore,
		ObjectUploader:     objUploader,
		ArgoCallbackSecret: "", // C-01 fix: callback route removed
		EventStore:         eventStore,
		BuildStoreReader:   buildStoreReader,
		SecretStore:        secretStore,
		SecretK8sSync:      secretK8sMgr,
		BootstrapStore:     bootstrapStore,
		BootstrapSecret:    cfg.BootstrapSecret,
		BuildHandler:        buildHandler,
		ArgoCallbackHandler: argoCallbackHandler,
		LogsHandler:        logsHandler,
		RollbackHandler:    rollbackHandler,
		StatusHandler:      statusHandler,
		ShutdownCtx:        shutdownCtx,
		ShutdownCancel:     shutdownCancel,
		RateLimiter:        rateLimiter,
		ReadHeaderTimeout:      cfg.ReadHeaderTimeout,
		ReadTimeout:            cfg.ReadTimeout,
		IdleTimeout:            cfg.IdleTimeout,
		ShutdownTimeout:        cfg.GracefulShutdownTime,
		SSEHeartbeatInterval:    cfg.SSEHeartbeatInterval,
		SSEPollInterval:         cfg.SSEPollInterval,
		SSESafetyTimeout:        cfg.SSESafetyTimeout,
		SSEMaxConcurrentStreams: int64(cfg.SSEMaxConcurrentStreams),
	}

	if err := server.Run(srvCfg); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
