// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/metrics"
	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/sse"
	"github.com/loopzero/loopzero/pkg/enforcement"
)

// NewRouter creates a chi router with middleware and routes.
func NewRouter(cfg Config) *chi.Mux {
	r := chi.NewRouter()

	// Security headers — before CORS so they are always present.
	r.Use(middleware.SecurityHeaders)

	// CORS — allow dashboard (cloud.{domain}) to call API (api.{domain}).
	// Must be early middleware so preflight OPTIONS get handled before auth.
	r.Use(corsMiddleware(cfg.CORSAllowedOrigin, cfg.IsProduction))

	// Middleware chain: metrics → otel-tracing → correlation-id → logging → backpressure
	r.Use(metrics.HTTPMetrics)
	r.Use(middleware.OtelHTTP)
	r.Use(middleware.CorrelationID)
	r.Use(middleware.RequestLogging(cfg.Logger))

	// PG backpressure: reject mutations when connection pool is near saturation.
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
	if cfg.CellDiscovery != nil {
		health.WithCellDiscovery(cfg.CellDiscovery)
	}
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
			// AUDIT-10: pass UsernameLookup so Whoami can resolve display-friendly
			// username from the users.username column (populated by migration 000037).
			auth := routes.NewAuthHandler(cfg.AuthUserLookup)
			r.Get("/api/v1/auth/whoami", auth.Whoami)

			// Rate limiting middleware chain — used for all mutation routes.
			rateLimitMiddlewares := make([]func(http.Handler) http.Handler, 0, 1)
			if cfg.RateLimiter != nil {
				rateLimitMiddlewares = append(rateLimitMiddlewares, cfg.RateLimiter.Middleware())
			}

			// Idempotent mutation middleware chain — rate limiting + idempotency.
			idempotentMiddlewares := make([]func(http.Handler) http.Handler, 0, 2)
			idempotentMiddlewares = append(idempotentMiddlewares, rateLimitMiddlewares...)
			if cfg.IdempotencyStore != nil {
				idempotentMiddlewares = append(idempotentMiddlewares, middleware.Idempotency(cfg.IdempotencyStore))
			}

			// Plan enforcement middlewares (Phase 1).
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
				Metric:  "builds_per_month",
				CountFn: enforcement.CountBuildsThisMonth(cfg.PlanLimiter),
			}, tenantFromCtx, upgradeURL)

			envEnforcement := enforcement.Middleware(cfg.PlanLimiter, enforcement.OperationCheck{
				Metric:  "environments",
				CountFn: enforcement.CountRows(cfg.PlanLimiter, "SELECT COUNT(*) FROM environment_targets WHERE tenant_id = $1"),
			}, tenantFromCtx, upgradeURL)

			seatEnforcement := enforcement.Middleware(cfg.PlanLimiter, enforcement.OperationCheck{
				Metric:  "seats",
				CountFn: enforcement.CountRows(cfg.PlanLimiter, "SELECT COUNT(*) FROM tenant_members WHERE tenant_id = $1"),
			}, tenantFromCtx, upgradeURL)

			registerProtectedRoutes(r, cfg, rateLimitMiddlewares, idempotentMiddlewares,
				projectEnforcement, buildEnforcement, envEnforcement, seatEnforcement)
		})
	}

	// C-01 fix: Callback route removed. Build completion is now detected by the
	// BuildReconcileLoop which polls Argo Workflow CRDs from the control plane.

	return r
}

// registerProtectedRoutes wires all authenticated API routes.
func registerProtectedRoutes(
	r chi.Router,
	cfg Config,
	rateLimitMiddlewares []func(http.Handler) http.Handler,
	idempotentMiddlewares []func(http.Handler) http.Handler,
	projectEnforcement, buildEnforcement, envEnforcement, seatEnforcement func(http.Handler) http.Handler,
) {
	// Source upload — auth + rate limiting, no idempotency.
	if cfg.SourceStore != nil {
		var sourceOpts []func(*routes.SourceHandler)
		if cfg.ObjectUploader != nil {
			sourceOpts = append(sourceOpts, routes.WithObjectUploader(cfg.ObjectUploader))
		}
		if cfg.IsProduction {
			sourceOpts = append(sourceOpts, routes.WithProductionMode())
		}
		if cfg.SourceBucket != "" {
			sourceOpts = append(sourceOpts, routes.WithSourceBucket(cfg.SourceBucket))
		}
		sourceHandler := routes.NewSourceHandler(cfg.SourceStore, sourceOpts...)
		r.With(rateLimitMiddlewares...).Post("/api/v1/source/upload", sourceHandler.Upload)
	}

	// Project routes
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

	// SSE stream route (read-only)
	if cfg.EventStore != nil && cfg.BuildStoreReader != nil {
		sseOpts := []sse.StreamOption{sse.WithLogger(cfg.Logger)}
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
	if cfg.BuildHandler != nil {
		r.Get("/api/v1/builds/{id}", cfg.BuildHandler.Get)
		r.With(append(idempotentMiddlewares, buildEnforcement)...).Post("/api/v1/builds", cfg.BuildHandler.Create)
		r.With(rateLimitMiddlewares...).Post("/api/v1/builds/{id}/cancel", cfg.BuildHandler.Cancel)
	}

	if cfg.LogsHandler != nil {
		r.Get("/api/v1/builds/{id}/logs", cfg.LogsHandler.Get)
	}
	if cfg.LogStreamHandler != nil {
		r.Get("/api/v1/builds/{id}/logs/stream", cfg.LogStreamHandler.StreamBuildLogs)
		r.Get("/api/v1/projects/{name}/logs/stream", cfg.LogStreamHandler.StreamRuntimeLogs)
	}
	if cfg.StatusHandler != nil {
		r.Get("/api/v1/projects/{name}/status", cfg.StatusHandler.Get)
	}
	if cfg.ScaleHandler != nil {
		r.With(rateLimitMiddlewares...).Post("/api/v1/projects/{name}/scale", cfg.ScaleHandler.Scale)
	}
	if cfg.BuildListHandler != nil {
		r.Get("/api/v1/projects/{name}/builds", cfg.BuildListHandler.List)
	}
	if cfg.RollbackHandler != nil {
		r.With(rateLimitMiddlewares...).Post("/api/v1/projects/{name}/rollback", cfg.RollbackHandler.Create)
	}
	if cfg.PromoteHandler != nil {
		r.With(rateLimitMiddlewares...).Post("/api/v1/projects/{name}/promote", cfg.PromoteHandler.Create)
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
		r.With(append(rateLimitMiddlewares, middleware.RequireAdmin)...).Post("/api/v1/tenants/bulk-delete", tenantHandler.Delete)
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

	if cfg.AuditHandler != nil {
		r.Get("/api/v1/audit", cfg.AuditHandler.List)
	}

	// Admin: bulk tenant eviction (T-02).
	if cfg.AdminEvictor != nil {
		r.Post("/api/v1/admin/tenants/evict", routes.AdminEvictHandler(cfg.AdminEvictor))
	}
}

// corsMiddleware adds CORS headers for the dashboard to call the API cross-origin.
// In production, if allowedOrigin is empty, no CORS headers are returned (deny all
// cross-origin requests). In development, an empty allowedOrigin permits any origin.
func corsMiddleware(allowedOrigin string, isProduction bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed := false
			if allowedOrigin != "" && origin == allowedOrigin {
				allowed = true
			} else if allowedOrigin == "" && !isProduction {
				// Dev only: open CORS when no origin is configured.
				allowed = true
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Correlation-ID")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
