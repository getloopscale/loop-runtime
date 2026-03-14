package server

import (
	"context"
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
	Logger             *slog.Logger
	DB                 routes.DBHealthChecker
	TokenValidator     middleware.TokenValidator
	MemberStore        middleware.MembershipStore
	ProjectStore       routes.ProjectStore
	TenantCRMgr        routes.TenantCRManager
	IdempotencyStore   middleware.IdempotencyStore
	SourceStore        source.SourceStore
	BuildOrchestrator  routes.BuildOrchestratorService
	ArgoCallbackSecret string
	EventStore         sse.EventStore
	BuildStoreReader   sse.BuildStoreReader
	SecretStore        routes.SecretStore
}

// NewRouter creates a chi router with middleware and routes.
func NewRouter(cfg Config) *chi.Mux {
	r := chi.NewRouter()

	// Middleware chain: metrics → correlation-id → logging
	r.Use(metrics.HTTPMetrics)
	r.Use(middleware.CorrelationID)
	r.Use(middleware.RequestLogging(cfg.Logger))

	// Prometheus metrics endpoint (no auth — standard for scraping)
	r.Handle("/metrics", promhttp.Handler())

	// Public routes (no auth required)
	health := routes.NewHealthHandler(cfg.DB)
	r.Get("/api/v1/health", health.Health)
	r.Get("/api/v1/health/deep", health.DeepHealth)

	// Protected routes (auth required)
	if cfg.TokenValidator != nil && cfg.MemberStore != nil {
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(cfg.TokenValidator, cfg.MemberStore))

			// Auth routes
			auth := routes.NewAuthHandler()
			r.Get("/api/v1/auth/whoami", auth.Whoami)

			// Source upload route
			if cfg.SourceStore != nil {
				sourceHandler := routes.NewSourceHandler(cfg.SourceStore)
				r.Post("/api/v1/source/upload", sourceHandler.Upload)
			}

			// Mutating routes (with idempotency middleware)
			if cfg.ProjectStore != nil && cfg.TenantCRMgr != nil {
				projects := routes.NewProjectHandler(cfg.ProjectStore, cfg.TenantCRMgr)
				if cfg.IdempotencyStore != nil {
					r.With(middleware.Idempotency(cfg.IdempotencyStore)).Post("/api/v1/projects", projects.Create)
				} else {
					r.Post("/api/v1/projects", projects.Create)
				}
			}

			// SSE stream route (no idempotency needed — read-only)
			if cfg.EventStore != nil && cfg.BuildStoreReader != nil {
				streamHandler := sse.NewStreamHandler(cfg.EventStore, cfg.BuildStoreReader,
				sse.WithLogger(cfg.Logger),
			)
				r.Get("/api/v1/builds/{id}/stream", streamHandler.ServeHTTP)
			}

			// Secret routes (read-only, no idempotency needed)
			if cfg.SecretStore != nil {
				secrets := routes.NewSecretHandler(cfg.SecretStore)
				r.Get("/api/v1/secrets", secrets.List)
			}

			// Build routes
			if cfg.BuildOrchestrator != nil {
				builds := routes.NewBuildHandler(cfg.BuildOrchestrator, cfg.ArgoCallbackSecret, cfg.Logger)

				// GET does not need idempotency
				r.Get("/api/v1/builds/{id}", builds.Get)

				// Mutating build routes with idempotency
				if cfg.IdempotencyStore != nil {
					r.With(middleware.Idempotency(cfg.IdempotencyStore)).Post("/api/v1/builds", builds.Create)
					r.With(middleware.Idempotency(cfg.IdempotencyStore)).Post("/api/v1/builds/{id}/cancel", builds.Cancel)
				} else {
					r.Post("/api/v1/builds", builds.Create)
					r.Post("/api/v1/builds/{id}/cancel", builds.Cancel)
				}
			}
		})
	}

	// Callback route — uses shared secret auth, NOT OAuth middleware.
	// Registered outside the auth group intentionally.
	if cfg.BuildOrchestrator != nil && cfg.ArgoCallbackSecret != "" {
		builds := routes.NewBuildHandler(cfg.BuildOrchestrator, cfg.ArgoCallbackSecret, cfg.Logger)
		r.Post("/api/v1/builds/{id}/callback", builds.Callback)
	}

	return r
}

// Run starts the HTTP server with graceful shutdown.
func Run(cfg Config) error {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	router := NewRouter(cfg)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cfg.Logger.Info("shutting down gracefully")
	return srv.Shutdown(ctx)
}
