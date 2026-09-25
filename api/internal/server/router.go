// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package server assembles the HTTP router (chi) + middleware
// pipeline + dependency wiring for the API binary. Routes are
// declared per resource family (protected_routes.go) and dispatched
// to the handlers under `api/internal/routes/`.
//
// What the router guarantees for every mutating route is rate limiting,
// and an Idempotency-Key where the route requires one; both are asserted
// route by route in mutation_guards_test.go. The environment lock
// (CLAUDE.md Rule 6) is taken by the handler or service that mutates a
// (tenant, project, environment), not by the router — the router cannot
// know which mutations touch one.
package server

import (
	"net/http"

	"github.com/getloopscale/loopzero/api/internal/auth/authhttp"
	"github.com/getloopscale/loopzero/api/internal/placement/placementhttp"
	"github.com/getloopscale/loopzero/api/internal/release/releasehttp"
	"github.com/getloopscale/loopzero/api/internal/version/versionhttp"

	"github.com/go-chi/chi/v5"

	planpkg "github.com/getloopscale/loopzero-contracts/plan"
	"github.com/getloopscale/loopzero/api/internal/bootstrap/bootstraphttp"
	"github.com/getloopscale/loopzero/api/internal/health/healthhttp"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	audit "github.com/getloopscale/loopzero/api/internal/middleware/audit"
	mwauth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	observability "github.com/getloopscale/loopzero/api/internal/middleware/observability"
	policy "github.com/getloopscale/loopzero/api/internal/middleware/policy"
	"github.com/getloopscale/loopzero/api/internal/surfaces"
)

// enforcementMetricBuildMinutes is the canonical metric label for the cost
// cap gating the POST /api/v1/builds route. Distinct from PlanLimiter's
// "builds_per_month" — see Wave 1 T1.9 for the SRP rationale.
const enforcementMetricBuildMinutes = planpkg.MetricBuildMinutes

// NewRouter creates a chi router with middleware and routes.
func NewRouter(cfg Config) *chi.Mux {
	if cfg.Auth == nil {
		cfg.Auth = mwauth.NewAuthenticator(mwauth.Config{})
	}
	authn := cfg.Auth
	r := chi.NewRouter()

	// Pre-auth IP rate limit (T2.4 of deep-review-remediation-plan, ADR D4).
	// Mounted FIRST so brute-force attempts on /api/v1/auth/* — or any other
	// route — are rejected at the edge without consuming downstream resources.
	// Trusted proxy CIDRs from env so production CF Tunnel can be honored
	// while dev k3d defaults to "trust nobody".
	if cfg.Guards.IPRateLimit != nil {
		r.Use(cfg.Guards.IPRateLimit.Middleware)
	}

	// Security headers — before CORS so they are always present.
	r.Use(policy.SecurityHeaders)

	// CORS — allow dashboard (cloud.{domain}) to call API (api.{domain}).
	// Must be early middleware so preflight OPTIONS get handled before auth.
	r.Use(corsMiddleware(cfg.CORSAllowedOrigin, cfg.IsProduction))

	// Maintenance mode: block mutating requests during infrastructure operations
	// (e.g., PG resize). Must come before auth so 503 is returned without token check.
	r.Use(policy.MaintenanceMode(cfg.Maintenance))

	// Middleware chain: metrics → otel-tracing → correlation-id → request-logger → logging → backpressure
	r.Use(metrics.HTTPMetrics)
	r.Use(observability.OtelHTTP)
	r.Use(observability.CorrelationID)
	// Beta Logs Gate: RequestLogger MUST come after CorrelationID so the
	// enrichment can read the id from ctx. Every handler's
	// `pkglogging.LoggerFromContext` now returns a logger bound with
	// correlation_id (+ trace_id when an extractor is configured).
	r.Use(observability.RequestLogger(cfg.ContextLogger))
	r.Use(observability.RequestLogging(cfg.Logger))

	// PG backpressure: reject mutations when connection pool is near saturation.
	if cfg.Guards.PGPoolGate != nil {
		pgCB := policy.NewPGCircuitBreaker(policy.PGCircuitBreakerConfig{
			Gate: cfg.Guards.PGPoolGate,
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
	health := healthhttp.NewHealthHandler(cfg.DB, cfg.HealthServices...)
	if cfg.CellDiscovery != nil {
		health.WithCellDiscovery(cfg.CellDiscovery)
	}
	r.Get("/api/v1/health", health.Health)
	r.Get("/api/v1/health/deep", health.DeepHealth)
	// B-123 — `/health/ready` is the ecosystem's name for this answer
	// (loopzero-contracts/httpsurface, D5). `deep` describes HOW the check is
	// implemented; `ready` describes what it answers, which is the question an
	// orchestrator asks before sending traffic.
	//
	// Same handler, not a copy: a second implementation of "check the database and
	// every registered service" would drift, and the half that drifts quietly is
	// degraded-detection — what keeps traffic off a node that cannot serve.
	//
	// `deep` stays. Deployment probes point at it and the maintenance middleware
	// special-cases the health prefix; renaming takes the service out of rotation
	// the moment this deploys. Removal is its own item after a deprecation deadline.
	r.Get("/api/v1/health/ready", health.DeepHealth)
	// F-07 (CHG-2026-04-29-DOGFOOD T5.2) — public endpoint, no auth.
	// Returns binary metadata (version, commit, build_date, go_version, platform).
	// Stable schema per Rule 5 (loopzero CLI --json contract analogue).
	r.Get("/api/v1/version", versionhttp.VersionHandler())
	// The status FLEET contract: the dashboard collector probes `/v1/version`
	// and reads `{version, built_at}`. Same truth as the endpoint above, in the
	// shape the other platform services already speak — without it the engine
	// does not appear on the /status panel. Public for the same reason: build
	// metadata is not a secret and the collector probes without a session.
	r.Get("/v1/version", versionhttp.FleetVersionHandler())

	// Internal-only routes (no auth — called by platform controllers, not users)
	if surfaces.Recorded(cfg.Surfaces, surfaces.Bootstrap, cfg.BootstrapStore != nil,
		"no database — the BootstrapStore is `PostgresBootstrapStore`, built by wirePlacement "+
			"(setup_server.go) on the cross-tenant background pool, which exists only with a database") {
		bootstrap := bootstraphttp.NewBootstrapHandler(cfg.BootstrapStore, cfg.BootstrapSecret, cfg.CellPlacement)
		r.Post("/internal/bootstrap", bootstrap.Bootstrap)
	}
	if surfaces.Recorded(cfg.Surfaces, surfaces.ArgoCallback, cfg.ArgoCallbackHandler != nil,
		"the orchestrator dependencies did not come up — the handler comes from `orchDeps` "+
			"(setup_server.go:236), which is nil when build orchestration was not assembled") {
		r.Post("/internal/argocd-callback", cfg.ArgoCallbackHandler.Handle)
	}
	// Layer 2 ESO sync broker (plan 2026-06-10-j22-openbao-transit-eso-sync
	// v1.2 D3 + plan 2026-06-02-openbao-adoption v1.3 T4.1).
	// /internal/v1/secret-fetch/{tenant_id}/{secret_name} is the endpoint
	// the ESO ClusterSecretStore webhook backend calls to resolve user
	// secrets — loopzero-api loads the vault:v1: ciphertext from PG +
	// decrypts via OpenBao transit + returns plaintext_b64. The handler
	// itself enforces secret_name regex (EC-08) + tenant↔cell binding
	// (EC-07) + writes audit_log. Bearer-token auth is provided by the
	// brokerBearerMiddleware wrap applied at construction in main.go.
	// nil-safe: when transit/store/cellStore/token are missing the
	// handler stays nil and the route is not registered — ESO sees 404
	// and retries with backoff (safer than registering a broken handler).
	if surfaces.Recorded(cfg.Surfaces, surfaces.SecretFetch, cfg.Secrets.FetchHandler != nil,
		"one of four dependencies is missing, and main.go:429-441 requires all four: the "+
			"secrets backend transit client, the database, the cross-tenant pool and `LOOPZERO_BROKER_TOKEN`. "+
			"Measured on dev-public 2026-08-16: the `loopzero-broker-token` Secret DOES NOT EXIST, "+
			"so this surface is already off there") {
		// T2.1 + ADR D2 of plan 2026-06-10-j22-layer2-code-review-fixes
		// (revised 2026-06-11 LIVE catch — ESO v1beta1 CRD rejects
		// `remoteRef.metadata` as unknown field): 2-segment URL where
		// {secret_name} is a composite `secretName.projectID.envName`
		// (EC-08 regex `^[A-Z][A-Z0-9_]*$` forbids `.` → safe delimiter).
		// Handler splits property into 3 parts and scopes broker SQL by
		// canonical composite unique key (closes HIGH #15 scope-drop).
		r.Post("/internal/v1/secret-fetch/{tenant_id}/{secret_name}", cfg.Secrets.FetchHandler)
	}

	// GitHub webhook (no auth — GitHub validates via HMAC signature internally)
	//
	// B-104: this decision is RECORDED, not just taken. Without the HMAC secret
	// `setup_github.go` leaves the handler nil, the route is never registered,
	// and the only trace is one Info line at startup — measured on the droplet as
	// a 404 here against a 405 on /health. In production that means a customer's
	// `git push` builds nothing, silently. The record surfaces as
	// `loopzero_api_surface_enabled{surface="github_webhook"}`.
	if cfg.GitHubWebhookHandler != nil {
		r.Post("/api/v1/webhooks/github", cfg.GitHubWebhookHandler.ServeHTTP)
		_ = cfg.Surfaces.Record(surfaces.GitHubWebhook, true, "")
	} else {
		_ = cfg.Surfaces.Record(surfaces.GitHubWebhook, false,
			"GITHUB_WEBHOOK_SECRET missing — the HMAC secret that validates GitHub deliveries")
	}

	// Admin endpoints — operator primitives, X-Service-Auth only (no user
	// context). Sibling of /webhooks/github so it bypasses the user-auth
	// middleware that requires identity headers.
	//
	// qa-sprint2-cleanup-2026-06-24 T1.2 (closes loopzero-cloud#16): /evict now
	// wraps the handler with Authenticator.RequireAdminService (chi canonical
	// r.With form). The middleware reads only LOOPZERO_ADMIN_SERVICE_AUTH_SECRET
	// (canonical) + _PREVIOUS — NOT the legacy LOOPZERO_SERVICE_AUTH_SECRET that
	// the cloud /engine/* proxy carries on user-JWT requests. The handler
	// still enforces LOOPZERO_ADMIN_API=true as a separate defense-in-depth gate.
	if surfaces.Recorded(cfg.Surfaces, surfaces.AdminEvict, cfg.Admin.Evictor != nil,
		"no database — `buildAdminEvictor` (setup_server.go:290) runs inside the block "+
			"that requires `dbAdapter`. A missing `loopzero-api-admin` Secret does NOT turn "+
			"this route off: it feeds the key the middleware checks, and without it "+
			"access is DENIED (fail closed), not opened") {
		r.With(authn.RequireAdminService).Post(
			"/api/v1/admin/tenants/evict",
			placementhttp.AdminEvictHandler(cfg.Admin.Evictor, cfg.Admin.APIEnabled),
		)
	}
	// CHG-2026-04-30-DELETE-SINGLE-CELL: pin tenants to specific cell.
	if surfaces.Recorded(cfg.Surfaces, surfaces.AdminPin, cfg.Admin.Pinner != nil,
		"no database — the CellPinner is built on `PostgresCellStore` "+
			"(setup_server.go:299). This is the ONLY route that moves a tenant between cells, "+
			"so turning it off does not degrade anything: it removes the operation") {
		r.With(authn.RequireAdminService).Post("/api/v1/admin/tenants/pin", placementhttp.AdminPinCellHandler(cfg.Admin.Pinner, cfg.AuditWriter, cfg.Admin.APIEnabled))
	}
	// T4.4 of plan 2026-05-28-meet-001-engineering-closure-plan.md:
	// diagnostic read-only endpoint for on-call to inspect a release's
	// signing + attestation chain without source access. Like every admin
	// route it sits behind Authenticator.RequireAdminService; the handler keeps only
	// the LOOPZERO_ADMIN_API feature gate.
	if surfaces.Recorded(cfg.Surfaces, surfaces.ReleaseInspect, cfg.Admin.ReleaseInspector != nil,
		"no database — built in setup_server.go:315, inside the block that requires "+
			"`dbAdapter`. It is on-call's view of a release's signing chain; turned off, "+
			"on-call loses the diagnosis exactly when it is needed") {
		r.With(authn.RequireAdminService).Get("/api/v1/admin/releases/{id}/inspect", releasehttp.AdminReleaseInspectHandler(cfg.Admin.ReleaseInspector, cfg.Admin.APIEnabled))
	}

	// Protected routes (auth required)
	//
	// The router's widest surface: turned off, EVERY client route disappears at
	// once. Measured: `TokenValidator` is built unconditionally
	// (setup_auth.go:75), so in production the guard depends only on
	// `MemberStore` — the condition exists so tests can mount a router without
	// auth.
	if surfaces.Recorded(cfg.Surfaces, surfaces.AuthProtected,
		cfg.TokenValidator != nil && cfg.MemberStore != nil,
		"no token validator or no MemberStore. In production the validator is "+
			"always built (setup_auth.go:75), so in practice this means "+
			"MemberStore is missing — which depends on the database") {
		r.Group(func(r chi.Router) {
			r.Use(mwauth.Auth(authn, cfg.TokenValidator, cfg.MemberStore))
			r.Use(scopeDatabaseToCaller)
			r.Use(audit.AuditLog(cfg.Logger, cfg.AuditWriter))

			// Auth routes
			// AUDIT-10: pass UsernameLookup so Whoami can resolve display-friendly
			// username from the users.username column (populated by migration 000037).
			// Plan T2.5 D8 lean: pass MemberStore so /whoami can list ALL of a
			// user's tenant memberships + mark the active team explicitly
			// (closes F-S2-02 silent team swap symptom).
			// Plan T2.5 D8 full: pass ActiveTeamStore so /whoami reads the
			// persistent users.active_tenant_id (migration 000018) and
			// /team/switch can update it. EC-4 self-heal lives in Whoami.
			auth := authhttp.NewAuthHandler(cfg.AuthUserLookup).
				WithMembershipLookup(cfg.MemberStore)
			if cfg.ActiveTeamStore != nil {
				auth = auth.WithActiveTeamStore(cfg.ActiveTeamStore)
			}
			r.Get("/api/v1/auth/whoami", auth.Whoami)

			chains := newRouteChains(cfg)

			// Plan T2.5 D8 full — POST /team/switch with rate limiting.
			if surfaces.Recorded(cfg.Surfaces, surfaces.ActiveTeam, cfg.ActiveTeamStore != nil,
				"no database — `NewPostgresActiveTeamStore` (setup_server.go) runs inside the "+
					"block that requires `dbAdapter`. Without it /whoami falls back to the "+
					"middleware heuristic and switching teams disappears") {
				r.With(chains.limited(audited("team.switched", "team"))...).Post("/api/v1/auth/team/switch", auth.SwitchTeam)
			}

			registerProtectedRoutes(r, cfg, chains)
		})
	}

	// C-01: the build-completion callback (build plane → API) was removed; build
	// completion is detected by the BuildReconcileLoop polling workflows from the
	// control plane. The sync callback above (/internal/argocd-callback) is a
	// different route and is live: GitOps sync notifications reach it.

	return r
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
				// Every method the router serves (cors_test.go keeps this true);
				// there is no PUT route, and PATCH changes a member's role.
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
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
