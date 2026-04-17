// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/loopzero/loopzero/api/internal/metrics"
	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/source"
	"github.com/loopzero/loopzero/api/internal/sse"
	"github.com/loopzero/loopzero/pkg/enforcement"
)

// Config holds the server configuration.
type Config struct {
	Port               string
	MetricsPort        string
	Logger             *slog.Logger
	DB                 routes.DBHealthChecker
	HealthServices     []routes.ServiceHealthChecker
	TokenValidator     middleware.TokenValidator
	MemberStore        middleware.MembershipStore
	ProjectStore       routes.ProjectStore
	TenantCRMgr        routes.TenantCRManager
	ProjectDeleter     routes.ProjectDeleter
	IdempotencyStore   middleware.IdempotencyStore
	SourceStore        source.SourceStore
	ObjectUploader     routes.ObjectUploader // optional: upload bundles to S3/Spaces for build access
	// ArgoCallbackSecret is deprecated — callback route removed (C-01 fix).
	// Kept in Config struct for backwards compatibility of env var parsing.
	ArgoCallbackSecret string
	EventStore         sse.EventStore
	BuildStoreReader   sse.BuildStoreReader
	SecretStore        routes.SecretStore
	// BootstrapStore handles platform bootstrap (internal-only endpoint).
	// When set, POST /internal/bootstrap is available.
	BootstrapStore     routes.BootstrapStore
	BootstrapSecret    string // SEC-003: shared secret for /internal/bootstrap auth
	CellPlacement      routes.CellPlacementEngine // ADR-0008: Cell assignment for new tenants
	// ShutdownCtx is cancelled when the server begins graceful shutdown.
	// Background goroutines (reaper, cleaner, pruner, callback processing)
	// should use this context so they stop before the connection pool closes (A-05).
	// Created in main.go and shared across all components that need shutdown awareness.
	ShutdownCtx        context.Context
	// ShutdownCancel cancels ShutdownCtx. Called by Run() on SIGTERM/SIGINT.
	ShutdownCancel     context.CancelFunc
	// BuildHandler is the single BuildHandler instance shared between auth
	// and callback routes. Created in main.go to avoid duplicate instances
	// with separate callbackWg (C-08).
	BuildHandler       *routes.BuildHandler
	// ArgoCallbackHandler handles POST /internal/argocd-callback from ArgoCD Notifications.
	ArgoCallbackHandler *routes.ArgoCallbackHandler
	// RateLimiter applies per-tenant rate limiting on mutation routes (C-01).
	// When nil, rate limiting is disabled.
	RateLimiter        *middleware.RateLimiter
	// HTTP server timeouts (configurable via env vars)
	ReadHeaderTimeout  time.Duration
	ReadTimeout        time.Duration
	IdleTimeout        time.Duration
	ShutdownTimeout    time.Duration
	// SSE stream configuration
	SSEHeartbeatInterval    time.Duration
	SSEPollInterval         time.Duration
	SSESafetyTimeout        time.Duration
	SSEMaxConcurrentStreams int64
	// LogsHandler handles GET /api/v1/builds/{id}/logs (build log retrieval via Loki).
	LogsHandler *routes.LogsHandler
	// RollbackHandler handles POST /api/v1/projects/{name}/rollback.
	RollbackHandler *routes.RollbackHandler
	// StatusHandler handles GET /api/v1/projects/{name}/status.
	StatusHandler *routes.StatusHandler
	// BuildListHandler handles GET /api/v1/projects/{name}/builds.
	BuildListHandler *routes.BuildListHandler
	// SecretK8sSync creates K8s Secrets on mgmt + ExternalSecret CRs on workload.
	SecretK8sSync routes.SecretK8sSync
	// EnvironmentHandler handles GET/POST/DELETE /api/v1/environments.
	EnvironmentHandler *routes.EnvironmentHandler
	// TeamHandler handles GET/POST/DELETE/PATCH /api/v1/teams/members.
	TeamHandler *routes.TeamHandler
	// DomainHandler handles custom domain CRUD + verification.
	// MEET-2026-04-05-033: Custom Domains feature.
	DomainHandler *routes.DomainHandler
	// GitHubWebhookHandler handles POST /api/v1/webhooks/github (no auth — GitHub sends events).
	GitHubWebhookHandler http.Handler
	// RepositoryHandler handles project↔GitHub repository linking.
	RepositoryHandler *routes.RepositoryHandler
	// IsProduction indicates production mode — enables strict error handling.
	IsProduction bool
	// AuditWriter writes mutation events to audit_log table. Nil = slog only.
	AuditWriter middleware.AuditWriter
	// AuditHandler handles GET /api/v1/audit (requires admin+ role).
	AuditHandler *routes.AuditHandler
	// TenantDeletionStore handles tenant soft-delete and tombstone records. MEET-052.
	TenantDeletionStore routes.TenantDeletionStore
	// AdminEvictor powers POST /api/v1/admin/tenants/evict (T-02).
	// Gated by LOOPZERO_ADMIN_API=true + X-Service-Auth header. Nil → route absent.
	// Plan: docs/plans/50k-immediate-focus.md
	AdminEvictor routes.TenantEvictor
	// PlanLimiter enforces plan limits on mutation routes (Phase 1).
	PlanLimiter *enforcement.PlanLimiter
	// UpgradeURL is included in 402 responses when plan limits are hit.
	UpgradeURL string
	// PGPoolStatsFunc returns (inUse, total) connection counts for backpressure.
	// When set, mutations are rejected with 503 when pool utilization > 90%.
	PGPoolStatsFunc func() (inUse int32, total int32)
	// PGCircuitBreakerThresholdPct is the pool saturation % for backpressure (default 90).
	PGCircuitBreakerThresholdPct int
}

// NewRouter creates a chi router with middleware and routes.
func NewRouter(cfg Config) *chi.Mux {
	r := chi.NewRouter()

	// CORS — allow dashboard (cloud.{domain}) to call API (api.{domain}).
	// Must be FIRST middleware so preflight OPTIONS get handled before auth.
	r.Use(corsMiddleware(os.Getenv("CORS_ALLOWED_ORIGIN")))

	// Middleware chain: metrics → otel-tracing → correlation-id → logging → backpressure
	// OtelHTTP MUST be before CorrelationID so span context is available,
	// but CorrelationID remains independent (C2 — MEET-020).
	r.Use(metrics.HTTPMetrics)
	r.Use(middleware.OtelHTTP)
	r.Use(middleware.CorrelationID)
	r.Use(middleware.RequestLogging(cfg.Logger))

	// PG backpressure: reject mutations when connection pool is near saturation.
	// Prevents cascading failures under load — returns 503 with Retry-After.
	if cfg.PGPoolStatsFunc != nil {
		threshold := cfg.PGCircuitBreakerThresholdPct
		if threshold == 0 {
			threshold = 90
		}
		pgCB := middleware.NewPGCircuitBreaker(middleware.PGCircuitBreakerConfig{
			ThresholdPct:  threshold,
			PoolStatsFunc: cfg.PGPoolStatsFunc,
		})
		r.Use(pgCB.Middleware())
	}

	// Custom 404/405 handlers: return JSON with LoopZeroError format (F-05)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"NOT_FOUND","message":"The requested endpoint does not exist."}`))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte(`{"code":"METHOD_NOT_ALLOWED","message":"This HTTP method is not supported for this endpoint."}`))
	})

	// Public routes (no auth required)
	health := routes.NewHealthHandler(cfg.DB, cfg.HealthServices...)
	r.Get("/api/v1/health", health.Health)
	r.Get("/api/v1/health/deep", health.DeepHealth)

	// Internal-only routes (no auth — called by platform controllers, not users)
	if cfg.BootstrapStore != nil {
		bootstrap := routes.NewBootstrapHandler(cfg.BootstrapStore, cfg.BootstrapSecret, cfg.CellPlacement)
		r.Post("/internal/bootstrap", bootstrap.Bootstrap)
	}
	if cfg.ArgoCallbackHandler != nil {
		r.Post("/internal/argocd-callback", cfg.ArgoCallbackHandler.Handle)
	}

	// GitHub webhook (no auth — GitHub validates via HMAC signature internally)
	if cfg.GitHubWebhookHandler != nil {
		r.Post("/api/v1/webhooks/github", cfg.GitHubWebhookHandler.ServeHTTP)
	}

	// Protected routes (auth required)
	if cfg.TokenValidator != nil && cfg.MemberStore != nil {
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(cfg.TokenValidator, cfg.MemberStore))
			r.Use(middleware.AuditLog(cfg.Logger, cfg.AuditWriter))

			// Auth routes
			auth := routes.NewAuthHandler()
			r.Get("/api/v1/auth/whoami", auth.Whoami)

			// Rate limiting middleware chain — used for all mutation routes.
			rateLimitMiddlewares := make([]func(http.Handler) http.Handler, 0, 1)
			if cfg.RateLimiter != nil {
				rateLimitMiddlewares = append(rateLimitMiddlewares, cfg.RateLimiter.Middleware())
			}

			// Idempotent mutation middleware chain — rate limiting + idempotency.
			// Only for routes that must be deduplicated (builds).
			idempotentMiddlewares := make([]func(http.Handler) http.Handler, 0, 2)
			idempotentMiddlewares = append(idempotentMiddlewares, rateLimitMiddlewares...)
			if cfg.IdempotencyStore != nil {
				idempotentMiddlewares = append(idempotentMiddlewares, middleware.Idempotency(cfg.IdempotencyStore))
			}

			// Plan enforcement middlewares (Phase 1).
			// Each mutation route gets a middleware that checks the plan limit before proceeding.
			// CountFn closures query the DB directly via PlanLimiter's DB connection.
			tenantFromCtx := func(r *http.Request) string {
				uc := middleware.GetUserContext(r.Context())
				if uc == nil {
					return ""
				}
				return uc.TenantID
			}
			upgradeURL := cfg.UpgradeURL
			if upgradeURL == "" {
				upgradeURL = "/billing"
			}

			projectEnforcement := enforcement.Middleware(cfg.PlanLimiter, enforcement.OperationCheck{
				Metric: "projects",
				CountFn: func(tenantID string) int {
					if cfg.ProjectStore == nil {
						return 0
					}
					ps, _ := cfg.ProjectStore.ListByTenant(context.Background(), tenantID)
					return len(ps)
				},
			}, tenantFromCtx, upgradeURL)

			buildEnforcement := enforcement.Middleware(cfg.PlanLimiter, enforcement.OperationCheck{
				Metric: "builds_per_month",
				CountFn: enforcement.CountBuildsThisMonth(cfg.PlanLimiter),
			}, tenantFromCtx, upgradeURL)

			envEnforcement := enforcement.Middleware(cfg.PlanLimiter, enforcement.OperationCheck{
				Metric: "environments",
				CountFn: enforcement.CountRows(cfg.PlanLimiter, "SELECT COUNT(*) FROM environment_targets WHERE tenant_id = $1"),
			}, tenantFromCtx, upgradeURL)

			seatEnforcement := enforcement.Middleware(cfg.PlanLimiter, enforcement.OperationCheck{
				Metric: "seats",
				CountFn: enforcement.CountRows(cfg.PlanLimiter, "SELECT COUNT(*) FROM tenant_members WHERE tenant_id = $1"),
			}, tenantFromCtx, upgradeURL)

			// Source upload — auth + rate limiting, no idempotency.
			// Upload is a pre-build step; idempotency belongs on build submission.
			if cfg.SourceStore != nil {
				var sourceOpts []func(*routes.SourceHandler)
				if cfg.ObjectUploader != nil {
					sourceOpts = append(sourceOpts, routes.WithObjectUploader(cfg.ObjectUploader))
				}
				if cfg.IsProduction {
					sourceOpts = append(sourceOpts, routes.WithProductionMode())
				}
				sourceHandler := routes.NewSourceHandler(cfg.SourceStore, sourceOpts...)
				r.With(rateLimitMiddlewares...).Post("/api/v1/source/upload", sourceHandler.Upload)
			}

			// Project routes — auth + rate limiting, no idempotency.
			// Project names are unique per tenant; the DB constraint handles duplicates.
			if cfg.ProjectStore != nil && cfg.TenantCRMgr != nil {
				projects := routes.NewProjectHandler(cfg.ProjectStore, cfg.TenantCRMgr)
				if cfg.ProjectDeleter != nil {
					projects.SetDeleter(cfg.ProjectDeleter)
				}
				r.Get("/api/v1/projects", projects.List)
				r.Get("/api/v1/projects/{name}", projects.Get)
				r.With(append(rateLimitMiddlewares, projectEnforcement)...).Post("/api/v1/projects", projects.Create)
				r.With(rateLimitMiddlewares...).Delete("/api/v1/projects/{name}", projects.Delete)
				r.With(rateLimitMiddlewares...).Delete("/api/v1/projects/{name}/environments/{env}", projects.DeleteEnvironment)
			}

			// SSE stream route (no idempotency needed — read-only)
			if cfg.EventStore != nil && cfg.BuildStoreReader != nil {
				sseOpts := []sse.StreamOption{
					sse.WithLogger(cfg.Logger),
				}
				if cfg.SSEHeartbeatInterval > 0 {
					sseOpts = append(sseOpts, sse.WithHeartbeatInterval(cfg.SSEHeartbeatInterval))
				}
				if cfg.SSEPollInterval > 0 {
					sseOpts = append(sseOpts, sse.WithPollInterval(cfg.SSEPollInterval))
				}
				if cfg.SSESafetyTimeout > 0 {
					sseOpts = append(sseOpts, sse.WithSafetyTimeout(cfg.SSESafetyTimeout))
				}
				if cfg.SSEMaxConcurrentStreams > 0 {
					sseOpts = append(sseOpts, sse.WithMaxConcurrentStreams(cfg.SSEMaxConcurrentStreams))
				}
				streamHandler := sse.NewStreamHandler(cfg.EventStore, cfg.BuildStoreReader, sseOpts...)
				r.Get("/api/v1/builds/{id}/stream", streamHandler.ServeHTTP)
			}

			// Secret routes
			if cfg.SecretStore != nil {
				secrets := routes.NewSecretHandler(cfg.SecretStore)
				if cfg.SecretK8sSync != nil {
					secrets.SetK8sSync(cfg.SecretK8sSync)
				}
				r.Get("/api/v1/secrets", secrets.List)
				r.With(rateLimitMiddlewares...).Post("/api/v1/secrets", secrets.Set)
				r.With(rateLimitMiddlewares...).Delete("/api/v1/secrets/{name}", secrets.DeleteSecret)
			}

			// Build routes — use the single BuildHandler from Config (C-08).
			// A single instance ensures one callbackWg tracks all in-flight
			// callbacks, so WaitForCallbacks drains them all on shutdown.
			if cfg.BuildHandler != nil {
				// GET does not need idempotency or rate limiting
				r.Get("/api/v1/builds/{id}", cfg.BuildHandler.Get)

				// Mutating build routes with rate limiting + idempotency
				r.With(append(idempotentMiddlewares, buildEnforcement)...).Post("/api/v1/builds", cfg.BuildHandler.Create)
				// Cancel is inherently idempotent (cancelling twice = same result).
				// Rate-limited but no Idempotency-Key required.
				r.With(rateLimitMiddlewares...).Post("/api/v1/builds/{id}/cancel", cfg.BuildHandler.Cancel)
			}

			// Build logs route (read-only, no idempotency needed)
			if cfg.LogsHandler != nil {
				r.Get("/api/v1/builds/{id}/logs", cfg.LogsHandler.Get)
			}

			// Status route (read-only, no idempotency needed)
			if cfg.StatusHandler != nil {
				r.Get("/api/v1/projects/{name}/status", cfg.StatusHandler.Get)
			}

			// Build list route (read-only, no idempotency needed)
			if cfg.BuildListHandler != nil {
				r.Get("/api/v1/projects/{name}/builds", cfg.BuildListHandler.List)
			}

			// Rollback route — mutation, needs rate limiting
			if cfg.RollbackHandler != nil {
				r.With(rateLimitMiddlewares...).Post("/api/v1/projects/{name}/rollback", cfg.RollbackHandler.Create)
			}

			// Team routes
			if cfg.TeamHandler != nil {
				r.Get("/api/v1/teams/members", cfg.TeamHandler.List)
				r.With(append(rateLimitMiddlewares, seatEnforcement)...).Post("/api/v1/teams/members", cfg.TeamHandler.Add)
				r.With(rateLimitMiddlewares...).Delete("/api/v1/teams/members/{username}", cfg.TeamHandler.Remove)
				r.With(rateLimitMiddlewares...).Patch("/api/v1/teams/members/{username}", cfg.TeamHandler.UpdateRole)
			}

			// Tenant deletion routes (MEET-052)
			if cfg.TenantDeletionStore != nil {
				tenantHandler := routes.NewTenantHandler(cfg.TenantDeletionStore, cfg.Logger)
				r.With(rateLimitMiddlewares...).Delete("/api/v1/tenants/{id}", tenantHandler.Delete)
				r.With(rateLimitMiddlewares...).Post("/api/v1/tenants/{id}/restore", tenantHandler.Restore)
				// Force-delete and bulk-delete require admin API key
				r.With(append(rateLimitMiddlewares, middleware.RequireAdmin)...).Post("/api/v1/tenants/bulk-delete", tenantHandler.Delete) // TODO: implement bulk handler
			}

			// Environment routes
			if cfg.EnvironmentHandler != nil {
				r.Get("/api/v1/environments", cfg.EnvironmentHandler.List)
				r.Get("/api/v1/environments/{name}", cfg.EnvironmentHandler.Get)
				r.With(append(rateLimitMiddlewares, envEnforcement)...).Post("/api/v1/environments", cfg.EnvironmentHandler.Create)
				r.With(rateLimitMiddlewares...).Delete("/api/v1/environments/{name}", cfg.EnvironmentHandler.Delete)
			}

			// Custom domain routes (MEET-2026-04-05-033)
			if cfg.DomainHandler != nil {
				r.Get("/api/v1/domains", cfg.DomainHandler.List)
				r.Get("/api/v1/domains/{domain}", cfg.DomainHandler.Get)
				r.With(append(rateLimitMiddlewares, idempotentMiddlewares...)...).Post("/api/v1/domains", cfg.DomainHandler.Create)
				r.With(rateLimitMiddlewares...).Post("/api/v1/domains/{domain}/verify", cfg.DomainHandler.Verify)
				r.With(rateLimitMiddlewares...).Delete("/api/v1/domains/{domain}", cfg.DomainHandler.Delete)
			}

			// Repository connection routes (GitHub App — auto-deploy)
			if cfg.RepositoryHandler != nil {
				r.Post("/api/v1/projects/{name}/connect", cfg.RepositoryHandler.Connect)
				r.Delete("/api/v1/projects/{name}/connect", cfg.RepositoryHandler.Disconnect)
				r.Get("/api/v1/projects/{name}/connect", cfg.RepositoryHandler.GetConnection)
			}

			// Audit log endpoint (requires admin+ role)
			if cfg.AuditHandler != nil {
				r.Get("/api/v1/audit", cfg.AuditHandler.List)
			}

			// Admin: bulk tenant eviction (T-02).
			// Feature-flagged via LOOPZERO_ADMIN_API env var checked inside handler.
			// Uses X-Service-Auth (not user auth) — bypasses the standard
			// auth chain via a nested Group so no user middleware runs.
			if cfg.AdminEvictor != nil {
				r.Post("/api/v1/admin/tenants/evict", routes.AdminEvictHandler(cfg.AdminEvictor))
			}
		})
	}

	// C-01 fix: Callback route removed. Build completion is now detected by the
	// BuildReconcileLoop which polls Argo Workflow CRDs from the control plane.
	// The build cluster no longer calls the API directly.

	return r
}

// NewMetricsServer creates a separate HTTP server for Prometheus metrics.
// This keeps /metrics off the main API port so it is not publicly accessible.
//
// SECURITY: The /metrics endpoint has no authentication. In Kubernetes, expose
// via ClusterIP Service only (not NodePort/LoadBalancer). For additional
// protection, use a NetworkPolicy to restrict access to the monitoring
// namespace (see infra/helm E-11 NetworkPolicy definitions).
func NewMetricsServer(port string) *http.Server {
	if port == "" {
		port = "9090"
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

// ValidateProductionEnv checks that all security-sensitive environment variables
// are set. Returns an error listing missing variables. Intended for use when
// LOOPZERO_ENV=production — in dev mode, fallback defaults are acceptable.
func ValidateProductionEnv(logger *slog.Logger, vars map[string]string) error {
	var missing []string
	for name, val := range vars {
		if val == "" {
			missing = append(missing, name)
			logger.Error("required environment variable not set", "var", name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %v", missing)
	}
	return nil
}

// Run starts the HTTP server with graceful shutdown.
func Run(cfg Config) error {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	// A-05: ShutdownCtx and ShutdownCancel are created in main.go and shared
	// with all background goroutines (reaper, cleaner, pruner, callbacks).
	// Run() calls ShutdownCancel on SIGTERM/SIGINT so goroutines stop before
	// the connection pool closes.
	if cfg.ShutdownCtx == nil || cfg.ShutdownCancel == nil {
		// Safety fallback — should not happen in production.
		ctx, cancel := context.WithCancel(context.Background())
		cfg.ShutdownCtx = ctx
		cfg.ShutdownCancel = cancel
	}

	router := NewRouter(cfg)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		// WriteTimeout intentionally omitted — SSE streams (/builds/{id}/stream)
		// are long-lived connections that must remain open for the duration of a
		// build (up to 30 minutes). Setting a server-wide WriteTimeout would kill
		// those connections prematurely.
		//
		// Individual mutation handlers enforce their own deadlines via
		// context.WithTimeout (150s), so non-SSE routes are still bounded:
		//   - routes/builds.go   → Create(), Cancel()
		//   - routes/source.go   → Upload()
		// Read-only routes (health, whoami, secrets list, build get) are
		// naturally short-lived and protected by ReadTimeout + IdleTimeout.
	}

	// Start metrics server on a separate port (not publicly accessible)
	metricsSrv := NewMetricsServer(cfg.MetricsPort)
	go func() {
		cfg.Logger.Info("metrics server starting", "port", metricsSrv.Addr)
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			cfg.Logger.Error("metrics server failed", "error", err)
		}
	}()

	// Graceful shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGTERM, syscall.SIGINT)

	errCh := make(chan error, 1)
	go func() {
		cfg.Logger.Info("server starting", "port", cfg.Port)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case sig := <-shutdown:
		cfg.Logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
	}

	// Step 1: Cancel the shared shutdown context. This signals ALL background
	// goroutines (reaper, cleaner, pruner, callback processors) to stop.
	// They must exit before we close the connection pool.
	cfg.ShutdownCancel()

	shutdownTimeout := cfg.ShutdownTimeout
	if shutdownTimeout == 0 {
		shutdownTimeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	cfg.Logger.Info("shutting down gracefully")

	// Step 2: Shut down HTTP servers (drain in-flight requests).
	if err := metricsSrv.Shutdown(ctx); err != nil {
		cfg.Logger.Error("metrics server shutdown failed", "error", err)
	}

	// Step 3: After Run() returns, main.go defers execute in LIFO order:
	//   a) bgWg wait (up to 30s for background goroutines to finish)
	//   b) dbAdapter.Close() (connection pool closed only after goroutines drain)
	return srv.Shutdown(ctx)
}

// corsMiddleware adds CORS headers for the dashboard to call the API cross-origin.
func corsMiddleware(allowedOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Allow specific origin or wildcard for dev
			allowed := false
			if allowedOrigin != "" && origin == allowedOrigin {
				allowed = true
			} else if allowedOrigin == "" {
				// No restriction configured — allow any *.loopzero.example
				allowed = true
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Correlation-ID")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			// Handle preflight
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
