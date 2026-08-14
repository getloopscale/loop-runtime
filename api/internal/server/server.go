// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server

import (
	"context"
	"errors"
	"github.com/getloopscale/loopzero/api/internal/domain"
	"github.com/getloopscale/loopzero/api/internal/routes/tenants"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/getloopscale/loopzero/api/internal/services/release"

	"github.com/getloopscale/loopzero/api/internal/middleware"
	"github.com/getloopscale/loopzero/api/internal/routes"
	"github.com/getloopscale/loopzero/api/internal/routes/audit"
	"github.com/getloopscale/loopzero/api/internal/routes/billing"
	"github.com/getloopscale/loopzero/api/internal/routes/domains"
	"github.com/getloopscale/loopzero/api/internal/routes/health"
	"github.com/getloopscale/loopzero/api/internal/routes/integrations"
	"github.com/getloopscale/loopzero/api/internal/routes/preview"
	"github.com/getloopscale/loopzero/api/internal/routes/repositories"
	"github.com/getloopscale/loopzero/api/internal/routes/scale"
	"github.com/getloopscale/loopzero/api/internal/routes/secrets"
	"github.com/getloopscale/loopzero/api/internal/source"
	"github.com/getloopscale/loopzero/api/internal/sse"
	pkgdomain "github.com/getloopscale/loopzero/pkg/domain"
	"github.com/getloopscale/loopzero/pkg/enforcement"
	pkglogging "github.com/getloopscale/loopzero/pkg/logging"
)

// Config holds the server configuration.
type Config struct {
	Port        string
	MetricsPort string
	Logger      *slog.Logger
	// ContextLogger is the request-scoped enriched logger factory used by
	// the RequestLogger middleware. When set, every HTTP handler can call
	// `pkglogging.LoggerFromContext(r.Context())` and the returned logger
	// automatically carries `correlation_id` + trace context.
	//
	// nil is tolerated — RequestLogger becomes a no-op and handlers
	// fall back to slog.Default. Production wiring in api/cmd/main.go
	// SHOULD set this (Beta Logs Gate requirement).
	ContextLogger  *pkglogging.ContextLogger
	DB             health.DBHealthChecker
	HealthServices []health.ServiceHealthChecker
	TokenValidator middleware.TokenValidator
	MemberStore    middleware.MembershipStore
	ProjectStore   routes.ProjectStore
	TenantCRMgr    routes.TenantCRManager
	ProjectDeleter routes.ProjectDeleter
	// PlanTierStore wires the canonical tier resolver (subscriptions.plan
	// active row → PlanTier enum per ADR D4 of plan
	// 2026-06-15-code-review-49-findings-remediation). nil-safe — when
	// nil, ProjectHandler/ScaleHandler fall back to PlanTierStarter
	// silently (the historical behavior). QA Run-3 #123 closure
	// requires this to be wired so admin@loopzero.example (team tier) is
	// served correctly.
	PlanTierStore pkgdomain.PlanTierStore
	// T4.1 of plan 2026-06-09-m2-operator-canonical-fix v1.4 (ADR D4):
	// optional EnvironmentEnsurer wired into the project Create handler
	// so every new project gets a `production` env auto-created. Reuses
	// the same EnvironmentEnsurer interface (and concrete
	// platform.EnvironmentEnsurer implementation) already wired by the
	// build handler — single source of truth for env creation / PG
	// environments row writes. nil-safe: when nil, ProjectHandler.Create
	// behaves identically to the pre-T4.1 handler (TestProjectCreate_BackwardCompatNoEnsurer).
	ProjectEnvEnsurer routes.EnvironmentEnsurer
	IdempotencyStore  middleware.IdempotencyStore
	SourceStore       source.SourceStore
	ObjectUploader    routes.ObjectUploader // optional: upload bundles to S3/Spaces for build access
	SourceBucket      string                // S3 bucket name for source bundles (env: SOURCE_BUCKET)
	// ArgoCallbackSecret is deprecated — callback route removed (C-01 fix).
	ArgoCallbackSecret string
	EventStore         sse.EventStore
	BuildStoreReader   sse.BuildStoreReader
	SecretStore        secrets.SecretStore
	BootstrapStore     domain.BootstrapStore
	BootstrapSecret    string                      // SEC-003: shared secret for /internal/bootstrap auth
	CellPlacement      domain.CellPlacementEngine  // ADR-0008: Cell assignment for new tenants
	CellDiscovery      health.CellDiscoveryChecker // MEET-053 A-04: optional — nil for StaticRouter
	ShutdownCtx        context.Context
	ShutdownCancel     context.CancelFunc
	BuildHandler       *routes.BuildHandler
	// CHG-2026-04-25-ATTESTATION-WIRE Phase 6: handler for
	// GET /api/v1/builds/{id}/attestation. Wired in api/cmd/setup_routes.go
	// against PostgresAttestationStore.
	BuildAttestationHandler *routes.BuildAttestationHandler
	// Sprint 6 / T1.2: handler for GET /api/v1/builds/{id}/sbom. Returns
	// per-app SBOM metadata + presigned download URLs from S3.
	BuildSBOMHandler    *routes.BuildSBOMHandler
	ArgoCallbackHandler *routes.ArgoCallbackHandler
	RateLimiter         *middleware.RateLimiter
	// IPRateLimit is the optional pre-auth IP-based rate limiter (T2.4).
	// When non-nil, mounted as the FIRST middleware so brute-force attempts
	// on /api/v1/auth/* are dropped at the edge.
	IPRateLimit *middleware.IPRateLimiter
	// VerifyRateLimiter is the per-(tenant, domain) limiter for
	// POST /api/v1/domains/{domain}/verify per J11D7 LOCKED ADR
	// (1 reverify per 30 seconds per (tenant, domain)).
	VerifyRateLimiter *middleware.VerifyRateLimiter
	// HTTP server timeouts (configurable via env vars)
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
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
	MetricsProxyHandler     *routes.MetricsProxyHandler // T1.2 J-18 — Prometheus query_range proxy
	StatusSSEHandler        *routes.StatusSSEHandler    // T2.1 J-18 T1.3.b — SSE signal.changed stream
	// DeployTimelineStore wires GET /api/v1/deploys/{deployID}/timeline
	// per plan deploy-timeline-canonical-5-phases T5.1. nil-safe: when
	// nil, the route is omitted (legacy / test paths). Production wires
	// aggregator.SSEEventSink-backed PostgresEventStore that satisfies
	// the DeployTimelineEventStore contract.
	DeployTimelineStore routes.DeployTimelineEventStore
	ScaleHandler        *scale.ScaleHandler
	BuildListHandler    *routes.BuildListHandler
	// BuildStatsHandler serves GET /api/v1/builds/stats — the tenant-scoped
	// deploys aggregate for the Overview sparkline (J-06 D2). Nil disables the route.
	BuildStatsHandler *routes.BuildStatsHandler
	SecretK8sSync     secrets.SecretK8sSync
	// SecretEnvLocker injects the environment lock into SecretHandler so
	// Set/DeleteSecret serialize with concurrent `loopzero deploy` flows.
	// Sprint A T1.1 (closes comp-001 CRITICAL — secret mutations were
	// running without the advisory lock). Wired from cmd/setup_server.go
	// using the same *adapters.PoolEnvironmentLocker injected into build
	// orchestrators. nil-safe: when nil, SecretHandler falls back to
	// the legacy non-locked path (test-friendly).
	SecretEnvLocker secrets.SecretEnvLocker
	// SecretProjectChecker injects the cross-tenant binding check (T1.2 of
	// plan 2026-06-10-j22-openbao-transit-eso-sync v1.2). When wired, Bulk
	// resolves the requested project's owning tenant via the tenant-scoped
	// GetByName and returns 404 PROJECT_NOT_FOUND for cross-tenant attempts
	// AND for nonexistent projects — byte-identical response (EC-03
	// existence-leak prevention / CWE-204 defense). nil-safe: when nil,
	// Bulk skips the check (legacy/test path). Production wires the
	// existing *store.PostgresProjectStore directly (signature matches).
	SecretProjectChecker secrets.SecretProjectChecker
	// SecretTransit injects the OpenBao transit encrypt path into the Bulk
	// handler (T2.3+T2.5 of plan 2026-06-10-j22-openbao-transit-eso-sync
	// v1.2). When wired, Bulk encrypts every entry.Value via Encrypt
	// BEFORE invoking store.Set so PG never receives plaintext bytes —
	// even if the store has its own vault dispatch (which it does in
	// the canonical wire path), encryptForStore detects the vault:v1:
	// prefix and passes through (no double-encrypt). Production wires
	// the same *vault.TransitClient that the secret store dispatches
	// through; nil disables the handler-layer encrypt (legacy AES path).
	SecretTransit secrets.SecretTransit
	// SecretFetchHandler exposes /internal/v1/secret-fetch/{tenant_id}/{name}
	// for ESO ClusterSecretStore webhook backend to resolve secrets via
	// loopzero-api (T4.1 of openbao-adoption v1.3). Wired by setup_secret_fetch.go
	// in api/cmd/. nil-safe: when nil, route is not registered (LIVE 404 ⇒
	// ESO retries with backoff — safer than registering a broken handler).
	SecretFetchHandler http.HandlerFunc
	EnvironmentHandler *routes.EnvironmentHandler
	TeamHandler        *routes.TeamHandler
	// AUDIT-10: narrow store for /whoami username lookup. Nil is legal (handler
	// omits username field). Implemented by store.PostgresTeamStore.
	AuthUserLookup routes.UsernameLookup
	// Plan T2.5 D8 full: persistent active-team selection. Nil is legal
	// (handler degrades to JWT middleware heuristic; /team/switch returns 501).
	ActiveTeamStore      routes.ActiveTeamStore
	DomainHandler        *domains.DomainHandler
	GitHubWebhookHandler http.Handler
	RepositoryHandler    *repositories.RepositoryHandler
	// Plan J-09 T2.1 (2026-06-05): dashboard repo picker source.
	IntegrationsGitHubHandler *integrations.IntegrationsGitHubHandler
	PreviewHandler            *preview.PreviewHandler // Sprint 5 / T2.3 — preview environments
	IsProduction              bool
	AuditWriter               middleware.AuditWriter
	AuditHandler              *audit.AuditHandler
	TenantDeletionStore       tenants.TenantDeletionStore
	// BillingHandler — T1.6 of deep-review-remediation-plan (closes
	// find_billing_route_not_wired HIGH). Wires GET /api/v1/billing and
	// POST /api/v1/billing/portal. Nil-safe: when nil, the routes are
	// skipped and CLI returns its own error rather than 404.
	BillingHandler *billing.BillingHandler
	AdminEvictor   routes.TenantEvictor
	AdminPinner    routes.CellPinner // CHG-2026-04-30-DELETE-SINGLE-CELL
	// T4.4 of plan 2026-05-28-meet-001-engineering-closure: diagnostic
	// read-only endpoint for on-call. Nil-safe — when unset, the route
	// is not mounted and the handler answers 404 like the other admin
	// routes. Wired by api/cmd/setup_server.go.
	ReleaseInspector release.ReleaseInspector
	PlanLimiter      *enforcement.PlanLimiter
	// CostEnforcer gates quantity-style metrics (egress bytes, registry pulls,
	// build minutes) against per-plan caps from cost_counters (migration 000017).
	// Distinct from PlanLimiter which gates rate-style limits (builds/month,
	// concurrent deploys). Nil-safe — when unset, the middleware pass-through.
	// Wired by api/cmd/setup_server.go::setupCostEnforcer (T1.9 production wiring).
	CostEnforcer                 *middleware.CostEnforcer
	UpgradeURL                   string
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
