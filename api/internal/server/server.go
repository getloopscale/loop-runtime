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
	BuildOrchestrator  routes.BuildOrchestratorService
	ArgoCallbackSecret string
	EventStore         sse.EventStore
	BuildStoreReader   sse.BuildStoreReader
	SecretStore        routes.SecretStore
	// HTTP server timeouts (configurable via env vars)
	ReadHeaderTimeout  time.Duration
	ReadTimeout        time.Duration
	IdleTimeout        time.Duration
	ShutdownTimeout    time.Duration
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

// NewMetricsServer creates a separate HTTP server for Prometheus metrics.
// This keeps /metrics off the main API port so it is not publicly accessible.
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
	router := NewRouter(cfg)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		// WriteTimeout intentionally omitted — SSE streams are long-lived.
		// Non-SSE routes should use http.TimeoutHandler per-route if needed.
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

	shutdownTimeout := cfg.ShutdownTimeout
	if shutdownTimeout == 0 {
		shutdownTimeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	cfg.Logger.Info("shutting down gracefully")

	// Shut down both servers
	if err := metricsSrv.Shutdown(ctx); err != nil {
		cfg.Logger.Error("metrics server shutdown failed", "error", err)
	}

	return srv.Shutdown(ctx)
}
