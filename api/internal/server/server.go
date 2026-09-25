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

	"github.com/getloopscale/loopzero/api/internal/auth/authhttp"
	"github.com/getloopscale/loopzero/api/internal/bootstrap"
	"github.com/getloopscale/loopzero/api/internal/environments/environmentshttp"
	"github.com/getloopscale/loopzero/api/internal/logs/logshttp"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/projectmetrics/projectmetricshttp"
	"github.com/getloopscale/loopzero/api/internal/projects"
	"github.com/getloopscale/loopzero/api/internal/routes/admin"
	"github.com/getloopscale/loopzero/api/internal/routes/argocd"
	"github.com/getloopscale/loopzero/api/internal/routes/builds"
	"github.com/getloopscale/loopzero/api/internal/routes/promote"
	"github.com/getloopscale/loopzero/api/internal/routes/rollback"
	"github.com/getloopscale/loopzero/api/internal/status/statushttp"
	"github.com/getloopscale/loopzero/api/internal/teams/teamshttp"
	"github.com/getloopscale/loopzero/api/internal/tenants/tenantshttp"
	"github.com/getloopscale/loopzero/api/internal/timeline/timelinehttp"

	"github.com/getloopscale/loopzero/api/internal/services/release"

	// Aliased because `routes/metrics` (the proxy route family, imported above)
	// already takes the name `metrics` in this file. Same collision as
	// `routes/source` vs `internal/source`, resolved the same way.
	"github.com/getloopscale/loopzero/api/internal/audit/audithttp"
	"github.com/getloopscale/loopzero/api/internal/billing/billinghttp"
	"github.com/getloopscale/loopzero/api/internal/health/healthhttp"
	"github.com/getloopscale/loopzero/api/internal/integrations/integrationshttp"
	appmetrics "github.com/getloopscale/loopzero/api/internal/metrics"
	mwaudit "github.com/getloopscale/loopzero/api/internal/middleware/audit"
	mwauth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	policy "github.com/getloopscale/loopzero/api/internal/middleware/policy"
	ratelimit "github.com/getloopscale/loopzero/api/internal/middleware/ratelimit"
	"github.com/getloopscale/loopzero/api/internal/preview/previewhttp"
	"github.com/getloopscale/loopzero/api/internal/repositories/repositorieshttp"
	"github.com/getloopscale/loopzero/api/internal/routes/domains"
	"github.com/getloopscale/loopzero/api/internal/scaling/scalinghttp"
	"github.com/getloopscale/loopzero/api/internal/secrets/secretshttp"
	"github.com/getloopscale/loopzero/api/internal/source"
	sourceroutes "github.com/getloopscale/loopzero/api/internal/source/sourcehttp"
	"github.com/getloopscale/loopzero/api/internal/sse"
	"github.com/getloopscale/loopzero/api/internal/surfaces"
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
	DB             healthhttp.DBHealthChecker
	HealthServices []healthhttp.ServiceHealthChecker
	TokenValidator mwauth.TokenValidator
	MemberStore    mwauth.MembershipStore
	ProjectStore   ports.ProjectStore
	TenantCRMgr    ports.TenantCRManager
	ProjectDeleter projects.Deleter
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
	ProjectEnvEnsurer promote.EnvironmentEnsurer
	SourceStore       source.SourceStore
	ObjectUploader    sourceroutes.ObjectUploader // optional: upload bundles to S3/Spaces for build access
	SourceBucket      string                      // S3 bucket name for source bundles (env: SOURCE_BUCKET)
	EventStore        sse.EventStore
	BuildStoreReader  sse.BuildStoreReader
	BootstrapStore    ports.BootstrapStore
	BootstrapSecret   string                          // SEC-003: shared secret for /internal/bootstrap auth
	CellPlacement     bootstrap.CellPlacementEngine   // ADR-0008: Cell assignment for new tenants
	CellDiscovery     healthhttp.CellDiscoveryChecker // MEET-053 A-04: optional — nil for StaticRouter
	ShutdownCtx       context.Context
	ShutdownCancel    context.CancelFunc
	BuildHandler      *builds.BuildHandler
	// CHG-2026-04-25-ATTESTATION-WIRE Phase 6: handler for
	// GET /api/v1/builds/{id}/attestation. Wired in api/cmd/setup_routes.go
	// against PostgresAttestationStore.
	BuildAttestationHandler *builds.BuildAttestationHandler
	// Sprint 6 / T1.2: handler for GET /api/v1/builds/{id}/sbom. Returns
	// per-app SBOM metadata + presigned download URLs from S3.
	BuildSBOMHandler    *builds.BuildSBOMHandler
	ArgoCallbackHandler *argocd.ArgoCallbackHandler
	LogsHandler         *logshttp.LogsHandler
	LogStreamHandler    *logshttp.LogStreamHandler
	RollbackHandler     *rollback.RollbackHandler
	PromoteHandler      *promote.PromoteHandler
	StatusHandler       *statushttp.StatusHandler
	MetricsProxyHandler *projectmetricshttp.MetricsProxyHandler // T1.2 J-18 — Prometheus query_range proxy
	StatusSSEHandler    *statushttp.StatusSSEHandler            // T2.1 J-18 T1.3.b — SSE signal.changed stream
	// DeployTimelineStore wires GET /api/v1/deploys/{deployID}/timeline
	// per plan deploy-timeline-canonical-5-phases T5.1. nil-safe: when
	// nil, the route is omitted (legacy / test paths). Production wires
	// aggregator.SSEEventSink-backed PostgresEventStore that satisfies
	// the DeployTimelineEventStore contract.
	DeployTimelineStore timelinehttp.DeployTimelineEventStore
	ScaleHandler        *scalinghttp.ScaleHandler
	BuildListHandler    *builds.BuildListHandler
	// BuildStatsHandler serves GET /api/v1/builds/stats — the tenant-scoped
	// deploys aggregate for the Overview sparkline (J-06 D2). Nil disables the route.
	BuildStatsHandler  *builds.BuildStatsHandler
	EnvironmentHandler *environmentshttp.EnvironmentHandler
	TeamHandler        *teamshttp.TeamHandler
	// AUDIT-10: narrow store for /whoami username lookup. Nil is legal (handler
	// omits username field). Implemented by store.PostgresTeamStore.
	AuthUserLookup authhttp.UsernameLookup
	// Plan T2.5 D8 full: persistent active-team selection. Nil is legal
	// (handler degrades to JWT middleware heuristic; /team/switch returns 501).
	ActiveTeamStore      authhttp.ActiveTeamStore
	DomainHandler        *domains.DomainHandler
	GitHubWebhookHandler http.Handler
	// Surfaces collects the router's registration decisions so a surface that
	// was skipped for a missing dependency can be SEEN — via
	// `loopzero_api_surface_enabled` — instead of only being discovered by whoever
	// hits the 404. Nil is legal and means "nobody is observing", which is what
	// a focused unit test wants; the production path wires it. B-104.
	Surfaces          *surfaces.Registry
	RepositoryHandler *repositorieshttp.RepositoryHandler
	// Plan J-09 T2.1 (2026-06-05): dashboard repo picker source.
	IntegrationsGitHubHandler *integrationshttp.IntegrationsGitHubHandler
	PreviewHandler            *previewhttp.PreviewHandler // Sprint 5 / T2.3 — preview environments
	IsProduction              bool
	AuditWriter               mwaudit.AuditWriter
	AuditHandler              *audithttp.AuditHandler
	TenantDeletionStore       tenantshttp.TenantDeletionStore
	// BillingHandler — T1.6 of deep-review-remediation-plan (closes
	// find_billing_route_not_wired HIGH). Wires GET /api/v1/billing and
	// POST /api/v1/billing/portal. Nil-safe: when nil, the routes are
	// skipped and CLI returns its own error rather than 404.
	BillingHandler    *billinghttp.BillingHandler
	CORSAllowedOrigin string // CORS_ALLOWED_ORIGIN — empty in production blocks all cross-origin requests
	// Timeouts are the HTTP server's timeouts.
	Timeouts HTTPTimeouts
	// SSE configures every live stream the router serves.
	SSE SSEConfig
	// Guards are the per-route protections the route chains are built from.
	Guards RouteGuards
	// Secrets carries what the secret routes and the secret broker need.
	Secrets SecretsConfig
	// Admin carries the collaborators of the operator (admin) routes.
	Admin AdminConfig
	// Auth is the authenticator every user route and admin route goes
	// through. Nil means an Authenticator with no secrets configured: user
	// routes still validate tokens, and admin routes refuse everyone.
	Auth *mwauth.Authenticator
	// Maintenance is LOOPZERO_MAINTENANCE_MODE, parsed at boot. The zero value is off.
	Maintenance policy.MaintenanceSetting
}

// HTTPTimeouts are the HTTP server's timeouts (configurable via env vars).
type HTTPTimeouts struct {
	ReadHeader time.Duration
	Read       time.Duration
	Idle       time.Duration
	// Shutdown bounds the graceful stop.
	Shutdown time.Duration
}

// SSEConfig configures the live streams (build, project, signals, logs,
// deploy timeline).
type SSEConfig struct {
	HeartbeatInterval    time.Duration
	PollInterval         time.Duration
	SafetyTimeout        time.Duration
	MaxConcurrentStreams int64
}

// RouteGuards are the protections the route chains are assembled from. Each
// is optional unless a route family says otherwise; a nil guard is skipped
// and, where it matters, recorded as a disabled surface.
type RouteGuards struct {
	// RateLimiter limits every authenticated mutation per tenant:user.
	RateLimiter *ratelimit.RateLimiter
	// IPRateLimit is the pre-auth per-IP limiter, mounted first so
	// brute-force attempts are dropped before authentication (T2.4).
	IPRateLimit *ratelimit.IPRateLimiter
	// VerifyRateLimiter limits POST /domains/{domain}/verify per
	// (tenant, domain) (J11D7: 1 re-verify per 30 seconds).
	VerifyRateLimiter *ratelimit.VerifyRateLimiter
	// IdempotencyStore backs the Idempotency-Key routes.
	IdempotencyStore policy.IdempotencyStore
	// PlanLimiter gates rate-style plan limits (builds/month, seats,
	// environments, concurrent deploys).
	PlanLimiter *enforcement.PlanLimiter
	// PlanUsage measures the usage PlanLimiter gates (builds this month,
	// permanent environments, seats). Set together with PlanLimiter; each
	// method is an enforcement.UsageCounter.
	PlanUsage PlanUsage
	// CostEnforcer gates quantity-style metrics (egress bytes, registry
	// pulls, build minutes) against per-plan caps. Nil passes through and
	// records the decision as `disabled`.
	CostEnforcer *policy.CostEnforcer
	// UpgradeURL is where a plan-limit refusal points the user.
	UpgradeURL string
	// PGPoolGate refuses mutations while the database pool is saturated
	// (hysteresis gate, B-054). Nil disables it.
	PGPoolGate policy.PoolGate
}

// SecretsConfig carries what the secret routes and the secret broker need.
type SecretsConfig struct {
	// Store persists secrets. Required for the secret routes.
	Store secretshttp.SecretStore
	// EnvLocker is the environment lock every secret mutation takes, so a
	// write serializes with a deploy capturing config. Required: without it
	// the secret routes are not mounted (a recorded disabled surface), never
	// mounted unlocked.
	EnvLocker secretshttp.SecretEnvLocker
	// K8sSync mirrors secrets to the runtime cluster. Optional.
	K8sSync secretshttp.SecretK8sSync
	// AuditLogger writes the SECRET_* audit rows. Nil only without a database.
	AuditLogger secretshttp.AuditLogger
	// ProjectChecker makes bulk writes answer an identical 404 for a
	// project of another tenant and a missing one (existence-leak
	// prevention). Nil skips the check.
	ProjectChecker secretshttp.SecretProjectChecker
	// FetchHandler serves /internal/v1/secret-fetch/{tenant_id}/{name}, the
	// broker the runtime cluster resolves secrets through. Nil leaves the
	// route unregistered (the caller retries on 404).
	FetchHandler http.HandlerFunc
}

// AdminConfig carries the collaborators of the operator (admin) routes; each
// route is mounted only when its collaborator is set.
type AdminConfig struct {
	// APIEnabled is the LOOPZERO_ADMIN_API feature flag: off, the admin routes
	// answer 404 even to an authorized operator.
	APIEnabled       bool
	Evictor          admin.TenantEvictor
	Pinner           admin.CellPinner
	ReleaseInspector release.ReleaseInspector
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

	// B-104: the router decides, per surface, whether to register it. Those
	// decisions used to vanish. A registry collects them, the gauge publishes
	// them, and the summary below names every surface that is off — so a missing
	// dependency is something operations can alert on rather than something a
	// customer discovers when their `git push` builds nothing.
	if cfg.Surfaces == nil {
		cfg.Surfaces = surfaces.NewRegistry()
	}

	router := NewRouter(cfg)

	appmetrics.PublishSurfaces(cfg.Surfaces.All())
	if off := cfg.Surfaces.Disabled(); len(off) > 0 && cfg.Logger != nil {
		for _, s := range off {
			cfg.Logger.Warn("http surface disabled — a dependency it needs is absent",
				"surface", s.Name, "reason", s.Reason)
		}
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: cfg.Timeouts.ReadHeader,
		ReadTimeout:       cfg.Timeouts.Read,
		IdleTimeout:       cfg.Timeouts.Idle,
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

	shutdownTimeout := cfg.Timeouts.Shutdown
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

// PlanUsage is the usage port the plan-limit middlewares count through. A
// counting error is returned, never read as zero: the middleware fails closed.
type PlanUsage interface {
	BuildsThisMonth(ctx context.Context, tenantID string) (int, error)
	PermanentEnvironments(ctx context.Context, tenantID string) (int, error)
	Seats(ctx context.Context, tenantID string) (int, error)
}

// unconfiguredPlanUsage stands in when a PlanLimiter is wired without a
// PlanUsage. Every count fails, so the middleware refuses the request rather
// than enforcing against a usage of zero. With no PlanLimiter the middleware
// passes through before counting, so dev boots are unaffected.
type unconfiguredPlanUsage struct{}

var errPlanUsageUnconfigured = errors.New("plan usage store is not configured")

func (unconfiguredPlanUsage) BuildsThisMonth(context.Context, string) (int, error) {
	return 0, errPlanUsageUnconfigured
}

func (unconfiguredPlanUsage) PermanentEnvironments(context.Context, string) (int, error) {
	return 0, errPlanUsageUnconfigured
}

func (unconfiguredPlanUsage) Seats(context.Context, string) (int, error) {
	return 0, errPlanUsageUnconfigured
}

// planUsage returns the configured PlanUsage, or one that refuses to count.
func (g *RouteGuards) planUsage() PlanUsage {
	if g.PlanUsage == nil {
		return unconfiguredPlanUsage{}
	}
	return g.PlanUsage
}
