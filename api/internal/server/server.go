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
)

// Config holds the server configuration.
type Config struct {
	Port               string
	MetricsPort        string
	Logger             *slog.Logger
	DB                 routes.DBHealthChecker
	TokenValidator     middleware.TokenValidator
	MemberStore        middleware.MembershipStore
	ProjectStore       routes.ProjectStore
	TenantCRMgr        routes.TenantCRManager
	IdempotencyStore   middleware.IdempotencyStore
	SourceStore        source.SourceStore
	ArgoCallbackSecret string
	EventStore         sse.EventStore
	BuildStoreReader   sse.BuildStoreReader
	SecretStore        routes.SecretStore
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
}

// NewRouter creates a chi router with middleware and routes.
func NewRouter(cfg Config) *chi.Mux {
	r := chi.NewRouter()

	// Middleware chain: metrics → correlation-id → logging
	r.Use(metrics.HTTPMetrics)
	r.Use(middleware.CorrelationID)
	r.Use(middleware.RequestLogging(cfg.Logger))

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
	health := routes.NewHealthHandler(cfg.DB)
	r.Get("/api/v1/health", health.Health)
	r.Get("/api/v1/health/deep", health.DeepHealth)

	// Protected routes (auth required)
	if cfg.TokenValidator != nil && cfg.MemberStore != nil {
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(cfg.TokenValidator, cfg.MemberStore))
			r.Use(middleware.AuditLog(cfg.Logger))

			// Auth routes
			auth := routes.NewAuthHandler()
			r.Get("/api/v1/auth/whoami", auth.Whoami)

			// Build the middleware chain for mutation routes.
			// Rate limiting (C-01) is applied before idempotency so rejected
			// requests never consume an idempotency key.
			mutationMiddlewares := make([]func(http.Handler) http.Handler, 0, 2)
			if cfg.RateLimiter != nil {
				mutationMiddlewares = append(mutationMiddlewares, cfg.RateLimiter.Middleware())
			}
			if cfg.IdempotencyStore != nil {
				mutationMiddlewares = append(mutationMiddlewares, middleware.Idempotency(cfg.IdempotencyStore))
			}

			// Source upload route
			if cfg.SourceStore != nil {
				sourceHandler := routes.NewSourceHandler(cfg.SourceStore)
				r.With(mutationMiddlewares...).Post("/api/v1/source/upload", sourceHandler.Upload)
			}

			// Mutating routes (with rate limiting + idempotency middleware)
			if cfg.ProjectStore != nil && cfg.TenantCRMgr != nil {
				projects := routes.NewProjectHandler(cfg.ProjectStore, cfg.TenantCRMgr)
				r.With(mutationMiddlewares...).Post("/api/v1/projects", projects.Create)
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

			// Secret routes (read-only, no idempotency needed)
			if cfg.SecretStore != nil {
				secrets := routes.NewSecretHandler(cfg.SecretStore)
				r.Get("/api/v1/secrets", secrets.List)
			}

			// Build routes — use the single BuildHandler from Config (C-08).
			// A single instance ensures one callbackWg tracks all in-flight
			// callbacks, so WaitForCallbacks drains them all on shutdown.
			if cfg.BuildHandler != nil {
				// GET does not need idempotency or rate limiting
				r.Get("/api/v1/builds/{id}", cfg.BuildHandler.Get)

				// Mutating build routes with rate limiting + idempotency
				r.With(mutationMiddlewares...).Post("/api/v1/builds", cfg.BuildHandler.Create)
				r.With(mutationMiddlewares...).Post("/api/v1/builds/{id}/cancel", cfg.BuildHandler.Cancel)
			}
		})
	}

	// Callback route — uses shared secret auth, NOT OAuth middleware.
	// Registered outside the auth group intentionally.
	// Uses the same BuildHandler instance so callbackWg is shared (C-08).
	if cfg.BuildHandler != nil && cfg.ArgoCallbackSecret != "" {
		r.Post("/api/v1/builds/{id}/callback", cfg.BuildHandler.Callback)
	}

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

	// Step 2: Wait for in-flight callbacks to drain (C-08).
	// The BuildHandler tracks goroutines via callbackWg.
	if cfg.BuildHandler != nil {
		cfg.BuildHandler.WaitForCallbacks(shutdownTimeout / 2)
	}

	// Step 3: Shut down HTTP servers (drain in-flight requests).
	if err := metricsSrv.Shutdown(ctx); err != nil {
		cfg.Logger.Error("metrics server shutdown failed", "error", err)
	}

	// Step 4: After Run() returns, main.go's defer dbAdapter.Close() runs,
	// closing the connection pool. By this point all goroutines have stopped.
	return srv.Shutdown(ctx)
}
