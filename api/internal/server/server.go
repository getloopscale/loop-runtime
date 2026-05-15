// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/source"
	"github.com/loopzero/loopzero/api/internal/sse"
	"github.com/loopzero/loopzero/pkg/enforcement"
	pkglogging "github.com/loopzero/loopzero/pkg/logging"
)

// Config holds the server configuration.
type Config struct {
	Port               string
	MetricsPort        string
	Logger             *slog.Logger
	// ContextLogger is the request-scoped enriched logger factory used by
	// the RequestLogger middleware. When set, every HTTP handler can call
	// `pkglogging.LoggerFromContext(r.Context())` and the returned logger
	// automatically carries `correlation_id` + trace context.
	//
	// nil is tolerated — RequestLogger becomes a no-op and handlers
	// fall back to slog.Default. Production wiring in api/cmd/main.go
	// SHOULD set this (Beta Logs Gate requirement).
	ContextLogger      *pkglogging.ContextLogger
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
	SourceBucket       string                // S3 bucket name for source bundles (env: SOURCE_BUCKET)
	// ArgoCallbackSecret is deprecated — callback route removed (C-01 fix).
	ArgoCallbackSecret string
	EventStore         sse.EventStore
	BuildStoreReader   sse.BuildStoreReader
	SecretStore        routes.SecretStore
	BootstrapStore     routes.BootstrapStore
	BootstrapSecret    string // SEC-003: shared secret for /internal/bootstrap auth
	CellPlacement      routes.CellPlacementEngine  // ADR-0008: Cell assignment for new tenants
	CellDiscovery      routes.CellDiscoveryChecker  // MEET-053 A-04: optional — nil for StaticRouter
	ShutdownCtx        context.Context
	ShutdownCancel     context.CancelFunc
	BuildHandler       *routes.BuildHandler
	// CHG-2026-04-25-ATTESTATION-WIRE Phase 6: handler for
	// GET /api/v1/builds/{id}/attestation. Wired in api/cmd/setup_routes.go
	// against PostgresAttestationStore.
	BuildAttestationHandler *routes.BuildAttestationHandler
	// Sprint 6 / T1.2: handler for GET /api/v1/builds/{id}/sbom. Returns
	// per-app SBOM metadata + presigned download URLs from S3.
	BuildSBOMHandler *routes.BuildSBOMHandler
	ArgoCallbackHandler *routes.ArgoCallbackHandler
	RateLimiter        *middleware.RateLimiter
	// IPRateLimit is the optional pre-auth IP-based rate limiter (T2.4).
	// When non-nil, mounted as the FIRST middleware so brute-force attempts
	// on /api/v1/auth/* are dropped at the edge.
	IPRateLimit        *middleware.IPRateLimiter
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
	LogsHandler             *routes.LogsHandler
	LogStreamHandler        *routes.LogStreamHandler
	RollbackHandler         *routes.RollbackHandler
	PromoteHandler          *routes.PromoteHandler
	StatusHandler           *routes.StatusHandler
	ScaleHandler            *routes.ScaleHandler
	BuildListHandler        *routes.BuildListHandler
	SecretK8sSync           routes.SecretK8sSync
	EnvironmentHandler      *routes.EnvironmentHandler
	TeamHandler             *routes.TeamHandler
	// AUDIT-10: narrow store for /whoami username lookup. Nil is legal (handler
	// omits username field). Implemented by db.PostgresTeamStore.
	AuthUserLookup          routes.UsernameLookup
	DomainHandler           *routes.DomainHandler
	GitHubWebhookHandler    http.Handler
	RepositoryHandler       *routes.RepositoryHandler
	PreviewHandler          *routes.PreviewHandler // Sprint 5 / T2.3 — preview environments
	IsProduction            bool
	AuditWriter             middleware.AuditWriter
	AuditHandler            *routes.AuditHandler
	TenantDeletionStore     routes.TenantDeletionStore
	// BillingHandler — T1.6 of deep-review-remediation-plan (closes
	// find_billing_route_not_wired HIGH). Wires GET /api/v1/billing and
	// POST /api/v1/billing/portal. Nil-safe: when nil, the routes are
	// skipped and CLI returns its own error rather than 404.
	BillingHandler *routes.BillingHandler
	AdminEvictor            routes.TenantEvictor
	AdminPinner             routes.CellPinner // CHG-2026-04-30-DELETE-SINGLE-CELL
	PlanLimiter             *enforcement.PlanLimiter
	// CostEnforcer gates quantity-style metrics (egress bytes, registry pulls,
	// build minutes) against per-plan caps from cost_counters (migration 000017).
	// Distinct from PlanLimiter which gates rate-style limits (builds/month,
	// concurrent deploys). Nil-safe — when unset, the middleware pass-through.
	// Wired by api/cmd/setup_server.go::setupCostEnforcer (T1.9 production wiring).
	CostEnforcer *middleware.CostEnforcer
	UpgradeURL              string
	PGPoolStatsFunc              func() (inUse int32, total int32)
	PGCircuitBreakerThresholdPct int
	CORSAllowedOrigin            string // CORS_ALLOWED_ORIGIN — empty in production blocks all cross-origin requests
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
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	// Step 1: Cancel the shared shutdown context. This signals ALL background
	// goroutines (reaper, cleaner, pruner, callback processors) to stop.
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
