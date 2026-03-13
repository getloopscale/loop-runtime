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

	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/routes"
)

// Config holds the server configuration.
type Config struct {
	Port             string
	Logger           *slog.Logger
	DB               routes.DBHealthChecker
	TokenValidator   middleware.TokenValidator
	MemberStore      middleware.MembershipStore
	ProjectStore     routes.ProjectStore
	TenantCRMgr      routes.TenantCRManager
	IdempotencyStore middleware.IdempotencyStore
}

// NewRouter creates a chi router with middleware and routes.
func NewRouter(cfg Config) *chi.Mux {
	r := chi.NewRouter()

	// Middleware chain: correlation-id → logging
	r.Use(middleware.CorrelationID)
	r.Use(middleware.RequestLogging(cfg.Logger))

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

			// Mutating routes (with idempotency middleware)
			if cfg.ProjectStore != nil && cfg.TenantCRMgr != nil {
				projects := routes.NewProjectHandler(cfg.ProjectStore, cfg.TenantCRMgr)
				if cfg.IdempotencyStore != nil {
					r.With(middleware.Idempotency(cfg.IdempotencyStore)).Post("/api/v1/projects", projects.Create)
				} else {
					r.Post("/api/v1/projects", projects.Create)
				}
			}
		})
	}

	return r
}

// Run starts the HTTP server with graceful shutdown.
func Run(cfg Config) error {
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
