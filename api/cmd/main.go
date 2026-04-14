// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package main is the entrypoint for the LoopZero API server.
package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx stdlib driver for sql.Open("pgx", ...)

	"github.com/loopzero/loopzero/pkg/enforcement"
	"github.com/loopzero/loopzero/pkg/logging"
	loopzerojwt "github.com/loopzero/loopzero/pkg/jwt"

	"github.com/loopzero/loopzero/api/internal/adapters"
	"github.com/loopzero/loopzero/api/internal/admission"
	"github.com/loopzero/loopzero/api/internal/tracing"
	"github.com/loopzero/loopzero/api/internal/config"
	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/domain"
	"github.com/loopzero/loopzero/api/internal/k8s"
	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/server"
	"github.com/loopzero/loopzero/api/internal/argo"
	"github.com/loopzero/loopzero/api/internal/argocd"
	"github.com/loopzero/loopzero/api/internal/gitops"
	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/api/internal/loki"
	"github.com/loopzero/loopzero/api/internal/placement"
	"github.com/loopzero/loopzero/api/internal/cfkv"
	"github.com/loopzero/loopzero/api/internal/cfzone"
	"github.com/loopzero/loopzero/api/internal/github"
	ghwebhook "github.com/loopzero/loopzero/api/internal/github/webhook"
	s3uploader "github.com/loopzero/loopzero/api/internal/s3"
	"github.com/loopzero/loopzero/api/internal/source"
	"github.com/loopzero/loopzero/api/internal/metrics"
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

	// Distributed tracing (MEET-2026-03-30-020)
	// C7: Controlled by LOOPZERO_TRACING_ENABLED env var.
	// Graceful degradation: API works without Collector.
	tracingShutdown, err := tracing.Init(context.Background(), "loopzero-api")
	if err != nil {
		logger.Warn("tracing disabled", "error", err)
	} else {
		defer func() {
			if err := tracingShutdown(context.Background()); err != nil {
				logger.Error("tracing shutdown failed", "error", err)
			}
		}()
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
	// JWT-only auth — uses pkg/jwt (single source of truth).
	// MEET-2026-04-07-039: B3+B4 — engine uses RSA-only with strict audience.
	// No HMAC session tokens accepted. Engine holds no signing secrets.
	jwksURL := os.Getenv("JWKS_URL")
	jwtIssuer := os.Getenv("JWT_ISSUER")
	rsaValidator := loopzerojwt.NewValidator(jwksURL, jwtIssuer, "loopzero-engine")

	// B3: Engine drops HMAC entirely — RSA-only validation.
	// Dashboard calls to engine must use Hydra RSA tokens or proxy through cloud.
	tokenValidator := middleware.NewJWTOnlyValidator(rsaValidator, nil)
	logger.Info("JWT auth enabled (RSA-only)", "jwks_url", jwksURL)

	// Service-to-service auth for internal proxy calls (cloud→engine).
	// Dashboard calls engine via cloud proxy with X-Service-Auth header.
	// S1.2: Dual-key rotation — accepts current OR previous during rotation window.
	middleware.ServiceAuthSecret = os.Getenv("LOOPZERO_SERVICE_AUTH_SECRET")
	middleware.ServiceAuthSecretPrevious = os.Getenv("LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS")

	// Tenant CR manager — real Kubernetes client for Tenant CRs.
	tenantCRMgr, err := k8s.NewTenantCRManager(cfg.MgmtKubeconfig, cfg.MgmtKubeContext)
	if err != nil {
		logger.Error("failed to create tenant CR manager", "error", err)
		os.Exit(1)
	}

	// Application CR manager — creates LoopZeroApplication CRs on mgmt cluster.
	// Reuses the TenantCRManager's dynamic client (same cluster, same RBAC).
	// Empty namespace falls back to the package-level tenantNamespace (same as TenantCRManager).
	appCRMgr := &appCRAdapter{mgr: k8s.NewApplicationCRManager(tenantCRMgr.Client, "")}

	// Secret K8s manager — creates K8s Secrets on mgmt + ExternalSecret CRs on workload for ESO sync.
	// F-01 fix: Use interface variable to avoid Go nil interface trap.
	// A typed nil (*SecretK8sManager)(nil) passes `!= nil` interface checks,
	// causing panic in CreateSecret. Only assign to interface on success.
	var secretK8sSync routes.SecretK8sSync
	if mgr, err := k8s.NewSecretK8sManager(
		cfg.MgmtKubeconfig, cfg.MgmtKubeContext,
		cfg.RuntimeKubeconfig, cfg.RuntimeKubeContext,
	); err != nil {
		logger.Warn("secret K8s manager not available — ESO sync disabled", "error", err)
	} else {
		secretK8sSync = mgr
	}

	// Database connection (optional — graceful degradation if not configured)
	var memberStore middleware.MembershipStore
	var projectStore routes.ProjectStore
	var dbHealth routes.DBHealthChecker
	var dbAdapter *db.PgxAdapter
	var planLimiter *enforcement.PlanLimiter

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
	var buildListHandler *routes.BuildListHandler
	var projectDeleter routes.ProjectDeleter
	var cellRouter placement.Router = placement.NewPassthroughRouter(placement.CellConfig{CellID: placement.DefaultCellID}) // fallback
	var cellStore *db.PostgresCellStore // implements both TenantCellQuerier and CellMetricsQuerier
	var domainHandler *routes.DomainHandler // MEET-2026-04-05-033: Custom Domains
	var githubWebhookHandler http.Handler   // GitHub App webhook handler (Phase: GitHub Integration)
	var repoHandler *routes.RepositoryHandler // GitHub repo↔project linking
	var orch *services.BuildOrchestrator                // Build orchestrator (shared with auto-deploy)
	var ghAppClient *github.AppClient                   // GitHub App client (shared with status notifier)

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

		// PG pool stats collector — exports connection metrics every 15s.
		// Feeds LoopZeroDBPoolNearExhaustion/LoopZeroDBPoolExhaustion alerts.
		poolCollector := metrics.NewPoolStatsCollector(dbAdapter.Pool, logger)
		poolCollector.Start(context.Background(), 15*time.Second)
		defer poolCollector.Stop()

		// Plan enforcement limiter (uses stdlib sql.DB for simple queries)
		stdDB, err := sql.Open("pgx", dbURL)
		if err != nil {
			logger.Warn("failed to open stdlib DB for plan enforcement — limits disabled", "error", err)
		} else {
			stdDB.SetMaxOpenConns(2)
			defer func() { _ = stdDB.Close() }()
			// Verify connection works before wiring
			if pingErr := stdDB.Ping(); pingErr != nil {
				logger.Warn("plan enforcement DB ping failed — limits disabled", "error", pingErr)
			} else {
				planLimiter = enforcement.NewPlanLimiter(stdDB, logger)
				logger.Info("plan enforcement enabled")
			}
		}

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
			Kubeconfig:        cfg.BuildKubeconfig,
			Context:           cfg.BuildKubeContext,
			RegistryURL:       cfg.RegistryURL,
			RegistryInsecure:  cfg.RegistryInsecure,
			BaseDomain:        cfg.BaseDomain,
			HarborRegistryURL: cfg.HarborRegistryURL,
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
		// ADR-0008 Phase 1+: Replace single syncClient with SyncClientFactory per Cell.
		// For now (single Cell), the circuit-breaker-wrapped client is used directly.
		// When multi-cell activates, create SyncClientFactory(cellRouter, builderFn)
		// and wrap each Cell's client with its own circuit breaker.
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

		orch = services.NewBuildOrchestrator(services.BuildOrchestratorDeps{
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
			KVClient:          kvClient,
			BaseDomain:        cfg.BaseDomain,
		})

		buildOrchestrator = &adapters.BuildOrchestratorAdapter{
			Orch:       orch,
			BuildStore: buildStore,
		}

		buildStoreReader = &adapters.BuildStoreReaderAdapter{Store: buildStore}

		// Build logs via Loki — uses a dedicated HTTP client with longer timeout.
		// Loki queries cross-cluster (mgmt → build NodePort) can take 15-30s for
		// large builds. The default httpClient has 10s timeout which causes
		// "context deadline exceeded" on most queries.
		lokiHTTPClient := &http.Client{
			Timeout:   30 * time.Second,
			Transport: httpTransport,
		}
		lokiClient := loki.NewClient(cfg.LokiURL, lokiHTTPClient, logger)
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

		// Build list handler — lists builds for a project with optional env filter
		buildListHandler = routes.NewBuildListHandler(&adapters.BuildListStoreAdapter{Store: buildStore})

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

		// Domain store — declared early because both build reconcile loop and
		// domain provisioner need it (MEET-2026-04-06-034).
		domainStore := &db.PostgresDomainStore{DB: dbAdapter}

		// C-01/C-03 fix: BuildReconcileLoop replaces both the HTTP callback
		// and the StartupReconciler. The control plane POLLS the build cluster's
		// Workflow CRDs instead of the build cluster calling the API.
		reconcileLoop := services.NewBuildReconcileLoop(
			buildStore,
			workflowSubmitter,
			orch,
			locker,
			domainStore, // MEET-034: custom domains in manifest rendering
			appCRMgr,    // creates LoopZeroApplication CRs on build completion
			pgEventStore,
			logger,
			services.BuildReconcileLoopConfig{
				Interval:           cfg.ReconcileInterval,
				RegistryURL:        cfg.RegistryURL,
				BaseDomain:         cfg.BaseDomain,
				MaxBuildsPerTenant: cfg.MaxBuildsPerTenant,
			},
		)

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

		// Idempotency cleaner — prunes expired keys every hour (A-08)
		idempotencyCleaner := services.NewIdempotencyCleaner(
			&db.PostgresIdempotencyStore{DB: dbAdapter},
			logger,
			1*time.Hour,
		)

		// SSE event pruner — removes events older than 24h every hour (C-06)
		eventPruner := sse.NewEventPruner(
			pgEventStore,
			logger,
			24*time.Hour,
			1*time.Hour,
		)

		// MEET-2026-03-31-023 T2-11: Leader election guard.
		// Background loops (reconcile, reapers, convergence) MUST run on exactly
		// one API replica. This advisory lock prevents duplicate loop execution
		// if the Deployment is accidentally scaled to 2+ replicas.
		//
		// CRITICAL: pg_try_advisory_lock is SESSION-scoped — the lock persists only
		// while the underlying database CONNECTION is open. Using pool.QueryRow()
		// acquires a connection, executes, and returns it to the pool — releasing
		// the lock immediately. We must use pool.Acquire() to hold a dedicated
		// connection for the entire process lifetime.
		const leaderLockID int64 = 0x7468656F6C656164 // "loopzerolead" in hex
		var isLeader bool

		// Acquire a dedicated connection from the pool. This connection will NOT
		// be returned to the pool until explicitly released, keeping the advisory
		// lock held for the entire process lifetime.
		leaderConn, err := dbAdapter.Pool.Acquire(context.Background())
		if err != nil {
			logger.Error("failed to acquire database connection for leader election", "error", err)
			os.Exit(1)
		}
		defer leaderConn.Release() // Released on process shutdown (after bgWg.Wait)

		// MEET-2026-04-09-045: Removed pg_advisory_unlock stale-lock cleanup.
		// DistSys review: if pod B starts on a PG connection previously used by pod A
		// (connection pool reuse across restarts), the unlock releases pod A's ACTIVE
		// lock — creating a window where NEITHER pod holds leadership.
		// PG cleans dead sessions automatically via TCP keepalive (15s × 3 = 45s).

		// Leader election with retry — waits up to 60s for the lock to become available.
		// MEET-2026-04-09-045: Increased from 6→12 attempts (60s total) to cover
		// worst-case TCP keepalive detection window on DO Managed PG (45-90s).
		logger.Info("starting leader election", "lockID", leaderLockID, "maxAttempts", 12)
		for attempt := 0; attempt < 12; attempt++ {
			if err := leaderConn.QueryRow(context.Background(),
				"SELECT pg_try_advisory_lock($1)", leaderLockID).Scan(&isLeader); err != nil {
				logger.Error("failed to acquire leader lock", "error", err)
				os.Exit(1)
			}
			if isLeader {
				break
			}
			logger.Info("leader lock held by another instance, retrying...", "attempt", attempt+1, "max", 12)
			time.Sleep(5 * time.Second)
		}

		// MEET-2026-04-04-029 P1: Export leader status metric.
		if isLeader {
			metrics.LeaderStatus.Set(1)
			logger.Info("leader lock acquired — background loops will start on this replica")
		} else {
			metrics.LeaderStatus.Set(0)
			logger.Warn("another API instance holds the leader lock — background loops will NOT start on this replica",
				"hint", "The API can serve HTTP requests but reconcile/reaper loops are disabled. See docs/runbooks/api-scaling.md")

			// MEET-2026-04-09-043 P1: Periodic re-election for non-leader pods.
			// If PG failover kills the leader's connection, this pod can take over
			// instead of staying non-leader forever until manually restarted.
			go func() {
				ticker := time.NewTicker(60 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-shutdownCtx.Done():
						return
					case <-ticker.C:
						var acquired bool
						reelCtx, reelCancel := context.WithTimeout(context.Background(), 5*time.Second)
						if err := leaderConn.QueryRow(reelCtx,
							"SELECT pg_try_advisory_lock($1)", leaderLockID).Scan(&acquired); err != nil {
							reelCancel()
							continue // PG error — retry next tick
						}
						reelCancel()
						if acquired {
							logger.Info("non-leader pod acquired leader lock — restarting to enable background loops")
							metrics.LeaderStatus.Set(1)
							os.Exit(0) // Kubernetes restarts the pod, which starts as leader
						}
					}
				}
			}()
		}

		// T1-7: Drain-on-Shutdown — WaitGroup tracks all background goroutines.
		// On SIGTERM, shutdownCtx is cancelled (goroutines stop), then we wait
		// up to 30s for them to finish before closing the DB pool.
		// Defers run LIFO, so this defer runs BEFORE defer dbAdapter.Close() (line 167).
		var bgWg sync.WaitGroup

		// ADR-0008: Cell Router — resolves cell_id to cluster connection details.
		// Phase 0/1: StaticRouter with single Cell (backward compat).
		// Phase 2+: populated from ClusterCredential CRDs or Pulumi outputs.
		staticRouter, err := placement.NewStaticRouter([]placement.CellConfig{{
			CellID:        placement.DefaultCellID,
			KubeConfig:    cfg.RuntimeKubeconfig,
			KubeContext:   cfg.RuntimeKubeContext,
			ArgocdURL:     cfg.ArgocdURL,
			ArgocdToken:   cfg.ArgocdToken,
			IngressDomain: cfg.BaseDomain,
		}})
		if err != nil {
			logger.Error("failed to create cell router", "error", err)
			os.Exit(1)
		}
		cellRouter = staticRouter

		// ADR-0008: Cell metrics collector (leader-only to avoid N identical PG queries).
		cellStore = &db.PostgresCellStore{DB: dbAdapter}
		cellMetrics := placement.NewCellMetricsCollector(
			cellStore,
			logger,
			60*time.Second,
		)

		// --- Custom Domains (MEET-2026-04-05-033, v2: CF Zone Management) ---
		// domainStore declared above (line ~387) for build reconcile loop.
		cnameTarget := "cname." + cfg.BaseDomain
		domainValidator := domain.NewValidator(cfg.BaseDomain, nil)
		dnsVerifier := &domain.DNSVerifier{}
		domainHandler = routes.NewDomainHandler(domainStore, dnsVerifier, domainValidator, cnameTarget)

		// v2: CF Zone Management — create zones per custom domain (model Vercel)
		cfZoneClient := cfzone.NewClient(cfg.CFAccountID, cfg.CFAPIToken)
		if cfg.CFZoneManagement && cfZoneClient != nil {
			logger.Info("custom domains: CF Zone Management enabled (v2)")
			domainHandler.WithCFZone(&cfZoneAdapter{client: cfZoneClient})
		} else {
			logger.Info("custom domains: TXT verification mode (v1)")
		}

		domainProvisioner := services.NewDomainProvisioner(
			domainStore, releaseStore, buildStore, gitopsPusher, locker, logger,
			services.DomainProvisionerConfig{
				Interval:    30 * time.Second,
				BaseDomain:  cfg.BaseDomain,
				RegistryURL: cfg.RegistryURL,
			},
		)
		if cfg.CFZoneManagement && cfZoneClient != nil {
			domainProvisioner.WithCFZone(&cfZoneStatusAdapter{client: cfZoneClient})
		}
		// --- GitHub App Webhook (auto-deploy on push) ---
		githubWebhookSecret := os.Getenv("GITHUB_WEBHOOK_SECRET")
		if githubWebhookSecret != "" {
			// GitHub App client for API calls (source download, commit status, PR comments)
			appIDStr := os.Getenv("GITHUB_APP_ID")
			appPrivateKey := os.Getenv("GITHUB_APP_PRIVATE_KEY")
			if appIDStr != "" && appPrivateKey != "" {
				var appID int64
				_, _ = fmt.Sscanf(appIDStr, "%d", &appID)
				var clientErr error
				ghAppClient, clientErr = github.NewAppClient(appID, appPrivateKey)
				if clientErr != nil {
					logger.Error("github app client creation failed", "error", clientErr)
				} else if ghAppClient != nil {
					logger.Info("github app client created", "app_id", appID)
				}
			}

			// Repository store for webhook lookups
			repoStore := &db.PostgresRepositoryStore{DB: dbAdapter}

			// Auto-deployer — handles push → download source → upload S3 → submit build
			var buildSub github.BuildSubmitter
			if orch != nil {
				buildSub = &buildSubmitterAdapter{
					orchestrator: orch,
					logger:       logger,
				}
			}
			autoDeployer := github.NewAutoDeployer(github.AutoDeployConfig{
				AppClient:      ghAppClient,
				ObjectStore:    objUploader,
				BuildSubmitter: buildSub,
				Logger:         logger,
			})

			wh := ghwebhook.NewHandler(githubWebhookSecret, logger)
			wh.OnPush(func(event ghwebhook.PushEvent) error {
				log := logger.With(
					"repo", event.Repository.FullName,
					"branch", event.Branch(),
					"sha", event.After,
					"sender", event.Sender.Login,
					"installation_id", event.Installation.ID,
				)

				// Ignore non-branch pushes (tags, deletes)
				if event.IsTag() || event.Deleted {
					log.Debug("github webhook: ignoring tag/delete push")
					return nil
				}

				// Only auto-deploy pushes to default branch (production)
				if !event.IsDefaultBranch() {
					log.Info("github webhook: push to non-default branch (preview env not yet supported)")
					return nil
				}

				// Look up linked project
				linked, err := repoStore.GetByRepo(context.Background(), event.Repository.FullName)
				if err != nil {
					log.Error("github webhook: repo lookup failed", "error", err)
					return nil // don't fail the webhook
				}
				if linked == nil {
					log.Debug("github webhook: repo not linked to any project, ignoring")
					return nil
				}
				if !linked.AutoDeploy {
					log.Info("github webhook: auto-deploy disabled for this project")
					return nil
				}

				log = log.With("project", linked.ProjectName, "tenant", linked.TenantID)
				log.Info("github webhook: auto-deploy triggered")

				// Trigger auto-deploy (async — don't block webhook response)
				commitMsg := ""
				if event.HeadCommit != nil {
					commitMsg = event.HeadCommit.Message
				}
				go func() {
					deployCtx := context.Background()
					if err := autoDeployer.HandlePush(deployCtx,
						event.Repository.FullName, event.Branch(), event.After,
						event.Installation.ID, linked.TenantID, linked.ProjectName,
						commitMsg, event.Sender.Login,
					); err != nil {
						log.Error("github webhook: auto-deploy failed", "error", err)
					}
				}()

				return nil
			})
			wh.OnPullRequest(func(event ghwebhook.PullRequestEvent) error {
				logger.Info("github webhook: pull_request received",
					"repo", event.Repository.FullName,
					"action", event.Action,
					"pr", event.Number,
					"branch", event.PullRequest.Head.Ref,
				)
				// TODO: Preview environments — deploy PR branch
				return nil
			})
			wh.OnInstallation(func(event ghwebhook.InstallationEvent) error {
				logger.Info("github webhook: installation event",
					"action", event.Action,
					"installation_id", event.Installation.ID,
					"account", event.Installation.Account.Login,
				)
				// Save/remove installation in DB
				switch event.Action {
				case "created":
					_ = repoStore.SaveInstallation(context.Background(),
						event.Installation.ID, event.Installation.Account.Login,
						event.Installation.Account.Type, nil)
				case "deleted":
					_ = repoStore.DeleteInstallation(context.Background(), event.Installation.ID)
				}
				return nil
			})
			githubWebhookHandler = wh
			repoHandler = routes.NewRepositoryHandler(&repoStoreAdapter{store: repoStore})
			logger.Info("github webhook: handler registered", "endpoint", "/api/v1/webhooks/github")

			// Register commit status callbacks (global — covers ALL activation paths)
			if ghAppClient != nil {
				statusNotifier := github.NewStatusNotifier(ghAppClient, &repoLookupAdapter{store: repoStore}, &buildLookupAdapter{store: buildStore}, cfg.BaseDomain, logger)
				if statusNotifier != nil {
					services.RegisterReleaseActivatedCallback(statusNotifier.OnBuildSucceeded)
					services.RegisterBuildFailedCallback(statusNotifier.OnBuildFailed)
					logger.Info("github: commit status callbacks registered (global)")
				}
			}
		} else {
			logger.Info("github webhook: GITHUB_WEBHOOK_SECRET not set, webhook disabled")
		}

		domainHealthChecker := services.NewDomainHealthChecker(
			domainStore, logger, services.DomainHealthCheckerConfig{
				CNAMETarget: cnameTarget,
				GraceDays:   14,
				StaleDays:   7,
			},
		)

		// Roadmap 02 — API Loop Extraction feature flag.
		// When LOOPZERO_DISABLE_LEGACY_LOOPS=true, the API does NOT start any
		// background loops even if it won the leader election. The controller
		// binary (cmd/controller) is expected to run them against the same
		// database. HTTP serving is unaffected. Default (flag unset) keeps
		// the pre-extraction behavior so every existing deployment is
		// unchanged. This is the documented rollback path: flip the env var
		// back to "false"/unset and restart the API Deployment.
		if isLeader && cfg.DisableLegacyLoops {
			logger.Info("LOOPZERO_DISABLE_LEGACY_LOOPS=true — API won leader election but loops will not run here; controller binary owns them")
			metrics.LeaderStatus.Set(0) // Report non-leader so Grafana/alerts treat controller as the leader
		}

		if isLeader && !cfg.DisableLegacyLoops {
			bgWg.Add(10) // 6 leader loops + 1 cell metrics + 1 leader heartbeat + 2 domain services
			go func() { defer bgWg.Done(); reconcileLoop.Start(shutdownCtx) }()
			go func() { defer bgWg.Done(); reaper.Start(shutdownCtx) }()
			go func() { defer bgWg.Done(); releaseReaper.Start(shutdownCtx) }()
			go func() { defer bgWg.Done(); releaseConvergenceLoop.Start(shutdownCtx) }()
			go func() { defer bgWg.Done(); idempotencyCleaner.Start(shutdownCtx) }()
			go func() { defer bgWg.Done(); eventPruner.Start(shutdownCtx) }()
			go func() { defer bgWg.Done(); cellMetrics.Start(shutdownCtx) }()
			go func() { defer bgWg.Done(); domainProvisioner.Start(shutdownCtx) }()
			go func() { defer bgWg.Done(); domainHealthChecker.Start(shutdownCtx) }()

			// MEET-2026-04-04-029 P1: Leader heartbeat — verify lock every 60s.
			// If the advisory lock is lost (pgx reconnect, PG failover), crash the
			// process so the container orchestrator restarts it with a fresh election.
			go func() {
				defer bgWg.Done()
				ticker := time.NewTicker(60 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-shutdownCtx.Done():
						return
					case <-ticker.C:
						var stillLeader bool
						// MEET-043 P0: 10s timeout prevents indefinite hang on zombie PG connection.
						// Without this, a silent PG failover causes the heartbeat to block forever,
						// and ALL background loops silently stop functioning.
						heartbeatCtx, heartbeatCancel := context.WithTimeout(context.Background(), 10*time.Second)
						if err := leaderConn.QueryRow(heartbeatCtx,
							"SELECT pg_try_advisory_lock($1)", leaderLockID).Scan(&stillLeader); err != nil {
							heartbeatCancel()
							logger.Error("leader heartbeat: lock check failed — exiting", "error", err)
							metrics.LeaderStatus.Set(0)
							os.Exit(1)
						}
						heartbeatCancel()
						if !stillLeader {
							logger.Error("leader heartbeat: lock LOST — exiting for re-election")
							metrics.LeaderStatus.Set(0)
							os.Exit(1)
						}
						// Lock still held — pg_try_advisory_lock is idempotent for the holder
						metrics.LeaderStatus.Set(1)
					}
				}
			}()
		}

		defer func() {
			waitDone := make(chan struct{})
			go func() {
				bgWg.Wait()
				close(waitDone)
			}()
			select {
			case <-waitDone:
				logger.Info("all background goroutines stopped")
			case <-time.After(30 * time.Second):
				logger.Warn("background goroutines did not stop within 30s, proceeding with shutdown")
			}
		}()

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
	defer rateLimiter.Stop() // SEC-007 fix: stop cleanup goroutine on shutdown

	// C-01 fix: BuildHandler no longer handles callbacks. The BuildReconcileLoop
	// detects build completion by polling Argo Workflow CRDs.
	var buildHandler *routes.BuildHandler
	if buildOrchestrator != nil {
		buildHandler = routes.NewBuildHandler(buildOrchestrator, logger)

		// Roadmap 07 — wire the build admission gate + pg pool circuit
		// breaker. Both respect LOOPZERO_ADMISSION_CONTROL (default off).
		if dbAdapter != nil {
			buildStoreForAdmission := db.NewPostgresBuildStore(dbAdapter)
			querier := &buildStoreAdmissionQuerier{store: buildStoreForAdmission}
			gate := admission.NewBuildGate(querier, admission.DefaultConfig(), cfg.AdmissionControl)
			buildHandler = buildHandler.WithAdmission(
				admission.NewRouteAdapter(gate),
				placement.DefaultCellID, // single-cell for now; roadmap 06 routing picks the cell per tenant
			)
			if cfg.AdmissionControl {
				logger.Info("build admission gate enabled",
					"max_per_cell", admission.DefaultConfig().MaxConcurrentPerCell,
					"max_per_tenant", admission.DefaultConfig().MaxConcurrentPerTenant,
				)
			}

			// PG pool circuit breaker — trips at 90% pool saturation.
			// Exports its state via metrics.LeaderStatus-style atomic bool;
			// future iterations will attach it to mutation middlewares.
			breaker := admission.NewPGPoolBreaker(
				dbAdapter.Pool,
				admission.DefaultBreakerConfig(cfg.DBMaxConns),
				logger,
			)
			breaker.Start(shutdownCtx)
			defer breaker.Stop()
			if cfg.AdmissionControl {
				logger.Info("pg pool circuit breaker enabled",
					"high_water", 0.90, "low_water", 0.75, "max_conns", cfg.DBMaxConns,
				)
			}
		}
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
		TokenValidator:   tokenValidator,
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
		SecretK8sSync:      secretK8sSync,
		BootstrapStore:     bootstrapStore,
		BootstrapSecret:    cfg.BootstrapSecret,
		// ADR-0008 bin-packing placement engine, serialized via a Postgres
		// advisory lock so concurrent tenant creations across API replicas
		// cannot both observe the same tenant counts and hot-spot a single
		// cell (roadmap 06 TOCTOU fix). When the pool is nil (local dev
		// without a DB) NewAdvisoryLockSerializer returns nil and the
		// engine falls back to its unserialized path.
		CellPlacement: placement.NewRoundRobinEngine(cellRouter, cellStore, 500).
			WithSerializer(placement.NewAdvisoryLockSerializer(dbAdapter.Pool)),
		BuildHandler:        buildHandler,
		ArgoCallbackHandler: argoCallbackHandler,
		LogsHandler:        logsHandler,
		RollbackHandler:    rollbackHandler,
		StatusHandler:      statusHandler,
		BuildListHandler:   buildListHandler,
		TeamHandler:        routes.NewTeamHandler(&db.PostgresTeamStore{DB: dbAdapter}),
		EnvironmentHandler: routes.NewEnvironmentHandler(tenantCRMgr),
		DomainHandler:      domainHandler,
		GitHubWebhookHandler: githubWebhookHandler,
		RepositoryHandler:    repoHandler,
		AuditHandler:       routes.NewAuditHandler(&db.PostgresAuditStore{DB: dbAdapter}),
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
		IsProduction:           cfg.IsProduction(),
		AuditWriter: func(tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) {
			if dbAdapter == nil {
				return
			}
			entry := db.AuditEntry{
				TenantID:      tenantID,
				Action:        action,
				Actor:         actor,
				ResourceType:  resourceType,
				ResourceID:    resourceID,
				CorrelationID: correlationID,
				Metadata:      metadata,
			}
			if err := db.WriteAuditLog(context.Background(), dbAdapter, entry); err != nil {
				logger.Warn("audit write failed", "error", err)
			}
		},
		PlanLimiter:            planLimiter,
		UpgradeURL:             os.Getenv("BILLING_UPGRADE_URL"),
	}

	if err := server.Run(srvCfg); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

// buildStoreAdmissionQuerier bridges *db.PostgresBuildStore to the
// admission gate's ActiveBuildsQuerier contract. Roadmap 07.
type buildStoreAdmissionQuerier struct {
	store *db.PostgresBuildStore
}

func (q *buildStoreAdmissionQuerier) ActiveBuilds(ctx context.Context, cellID string) (admission.ActiveBuildsSnapshot, error) {
	rows, err := q.store.FindActiveBuilds(ctx, cellID)
	if err != nil {
		return admission.ActiveBuildsSnapshot{}, err
	}
	byTenant := make(map[string]int, len(rows))
	for _, b := range rows {
		byTenant[b.TenantID]++
	}
	return admission.ActiveBuildsSnapshot{
		Total:    len(rows),
		ByTenant: byTenant,
	}, nil
}

// appCRAdapter bridges k8s.ApplicationCRManager to services.ApplicationCREnsurer.
type appCRAdapter struct {
	mgr *k8s.ApplicationCRManager
}

func (a *appCRAdapter) EnsureLoopZeroApplication(ctx context.Context, input services.ApplicationCRInput) error {
	return a.mgr.EnsureLoopZeroApplication(ctx, k8s.LoopZeroApplicationInput{
		TenantID:    input.TenantID,
		Project:     input.Project,
		Environment: input.Environment,
		AppName:     input.AppName,
		ServiceName: input.ServiceName,
		ServicePort: input.ServicePort,
	})
}
