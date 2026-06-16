// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Cloud Auth Service — handles authentication, OAuth2, and consent flows.
// Sits in front of the LoopZero engine API, validating JWTs and enforcing plans.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"

	loopzerojwt "github.com/getloopscale/loopzero-contracts/jwt"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	"github.com/loopzero/loopzero/cloud/internal/billing"
	"github.com/loopzero/loopzero/cloud/internal/config"
	"github.com/loopzero/loopzero/cloud/internal/consent"
	"github.com/loopzero/loopzero/cloud/internal/metering"
	cloudmetrics "github.com/loopzero/loopzero/cloud/internal/metrics"
	"github.com/loopzero/loopzero/cloud/internal/ratelimit"
	"github.com/loopzero/loopzero/cloud/internal/memory_webhook_worker"
	"github.com/loopzero/loopzero/cloud/internal/routes"
	"github.com/loopzero/loopzero/cloud/internal/themory"
	"github.com/loopzero/loopzero/cloud/internal/tracing"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Initialize OTel tracing (no-op if LOOPZERO_TRACING_ENABLED=false).
	tracingShutdown, err := tracing.Init(context.Background(), "loopzero-cloud")
	if err != nil {
		logger.Warn("tracing init failed (continuing without traces)", "error", err)
	}

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	// Initialize database connection (optional in dev, required in prod).
	// The cloud service shares the database with the engine API.
	// It does NOT run migrations — the API owns all schema changes.
	var db *sql.DB
	if cfg.DatabaseURL != "" {
		var err error
		db, err = sql.Open("pgx", cfg.DatabaseURL)
		if err != nil {
			logger.Error("failed to open database", "error", err)
			os.Exit(1)
		}
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(5 * time.Minute)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			logger.Warn("database not reachable at startup (will retry on requests)", "error", err)
		} else {
			logger.Info("database connected")
		}
	} else {
		logger.Warn("DATABASE_URL not set — billing, enforcement, and metering disabled")
	}

	// Initialize Hydra client
	hydra := auth.NewHydraClient(cfg.HydraPublicURL, cfg.HydraAdminURL, cfg.HydraClientID)

	// Register CLI OAuth2 client in Hydra (idempotent — safe on every startup).
	// Include both localhost (dev) and production dashboard callback URLs.
	// Register redirect URIs for PKCE callback.
	// Include localhost (dev) and dashboard URL (which may be served at a different domain).
	// The dashboard uses window.location.origin for redirect_uri, so we register both
	// the dashboard subdomain and the API gateway domain where it may also be served.
	redirectURIs := []string{
		"http://localhost:9876/auth",
		cfg.BillingReturnURL + "/login/callback",
	}
	// If Kratos public URL differs from BillingReturnURL, the dashboard may be
	// served at the Kratos domain (Option D: API gateway). Register that too.
	if cfg.KratosPublicURL != "" && cfg.KratosPublicURL != cfg.BillingReturnURL {
		redirectURIs = append(redirectURIs, cfg.KratosPublicURL+"/login/callback")
	}
	if err := hydra.RegisterClient(redirectURIs); err != nil {
		logger.Warn("failed to register OAuth2 client (Hydra may not be ready yet)", "error", err)
	}

	// JWT validation — single source of truth (pkg/jwt)
	jwksURL := cfg.HydraPublicURL + "/.well-known/jwks.json"
	jwtValidator := loopzerojwt.NewValidator(jwksURL, cfg.JWTIssuer, "loopzero-cloud")

	// Session token issuer/validator (HMAC JWTs for dashboard)
	// MEET-2026-04-07-039: audience explicit. §2.2: kid + dual-key rotation.
	sessIssuer := loopzerojwt.NewSessionTokenIssuer(
		cfg.SessionTokenSecret.Value(),
		"loopzero-cloud",
		"loopzero-cloud",
		15*time.Minute,
	).WithPreviousSecret(string(cfg.SessionTokenSecretPrevious))
	tenantProvisioner := auth.NewTenantProvisioner(db, logger)
	sessionTokenHandler := routes.NewSessionTokenHandler(sessIssuer, tenantProvisioner, cfg.KratosInternalURL)

	// Billing (optional — requires Stripe keys and DB)
	var stripeClient *billing.StripeClient
	var webhookHandler *billing.WebhookHandler
	var billingHandler *routes.BillingHandler
	var billingProvisioner *billing.Provisioner

	if cfg.StripeAPIKey.Value() != "" {
		stripeClient = billing.NewStripeClient(cfg.StripeAPIKey.Value())
		webhookHandler = billing.NewWebhookHandler(db, cfg.StripeWebhookSecret.Value(), billing.LoadPlanConfig(), logger)
		billingProvisioner = billing.NewProvisioner(db, stripeClient, logger)
		billingHandler = routes.NewBillingHandler(db, stripeClient, webhookHandler, billingProvisioner, cfg.BillingReturnURL)
		logger.Info("billing enabled")
	} else {
		logger.Warn("STRIPE_API_KEY not set — billing endpoints disabled")
	}

	// Universal JWT auth middleware (RSA + HMAC — one path for all)
	requireJWT := auth.RequireJWT(jwtValidator, sessIssuer)

	// Rate limiters (find_sec_007). Device endpoints are pre-auth → keyed on
	// client IP (chimw.RealIP already resolves the real client behind the proxy).
	// Device token is polled (~12/min legitimately) so its limit is generous.
	// Billing mutations are per authenticated user.
	deviceLimiter := ratelimit.NewInMemoryLimiter(60, time.Minute)
	billingLimiter := ratelimit.NewInMemoryLimiter(30, time.Minute)
	ipKey := func(r *http.Request) string { return ratelimit.Key("", r.RemoteAddr) }
	userKey := func(r *http.Request) string {
		if u := auth.GetUser(r.Context()); u != nil {
			return ratelimit.Key(u.Subject, r.RemoteAddr)
		}
		return ratelimit.Key("", r.RemoteAddr)
	}

	// Handlers
	deviceHandler := routes.NewDeviceHandler(hydra, cfg.BillingReturnURL)
	meHandler := routes.NewMeHandler()
	consentHandler := consent.NewHandler(hydra, tenantProvisioner, cfg.KratosPublicURL, cfg.KratosInternalURL, cfg.KratosAdminURL, logger)

	// Plan 0 of dashboard-memory-section-roadmap.md: unified loopzero_live_* API keys.
	// D8 dispatch: /v1/api-keys/* uses RequireJWT (PaaS session cookie), NOT
	// RequireScope. The scope-checked Bearer surface is the consumer side
	// (memory + future verticals), registered in subsequent plans.
	apiKeysHandler := routes.NewAPIKeysHandler(db, logger)

	// Plan 1 of dashboard-memory-section-roadmap.md: Memory Shell — Go proxy to
	// OSS TheMemory. Only instantiated when THEMORY_BASE_URL is configured,
	// matching the same opt-in pattern as billing (BillingHandler is nil when
	// Stripe is not configured). Operators must set THEMORY_BASE_URL +
	// THEMORY_API_KEY before /v1/memory/* routes are usable.
	var memoryHandler *routes.MemoryHandler
	var memoryWebhooksHandler *routes.MemoryWebhooksHandler
	var memoryExportsHandler *routes.MemoryExportsHandler
	if cfg.ThemoryBaseURL != "" {
		themoryHTTP := &http.Client{
			Timeout:   30 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport), // D4 W3C trace propagation
		}
		themoryClient := themory.NewClient(cfg.ThemoryBaseURL, cfg.ThemoryAPIKey.Value(), themoryHTTP)
		// Memory Webhook Delivery plan T5.6: wire the event publisher so
		// `memory.forgotten` mutations fan out to active webhooks.
		webhookEventPublisher := routes.NewMemoryWebhookEventPublisher(db, logger)
		memoryHandler = routes.NewMemoryHandler(themoryClient, logger).WithEventPublisher(webhookEventPublisher)
		logger.Info("memory proxy enabled", "themory_base_url", cfg.ThemoryBaseURL)

		// Plan 3 — Memory Operations (exports + webhooks).
		// Webhooks need only the DB; exports need DB + TheMemory client +
		// a worker-output directory. Both stay nil if Memory itself is
		// disabled, so the /v1/memory/exports/* and /v1/memory/webhooks/*
		// blocks below also remain unregistered when off.
		exportDir := os.Getenv("LOOPZERO_EXPORT_DIR")
		if exportDir == "" {
			exportDir = "/tmp/loopzero-exports"
		}
		workerDelay := 3 * time.Second
		if d := os.Getenv("EXPORT_WORKER_DELAY_MS"); d != "" {
			if parsed, perr := time.ParseDuration(d + "ms"); perr == nil {
				workerDelay = parsed
			}
		}
		memoryWebhooksHandler = routes.NewMemoryWebhooksHandler(db, logger)
		memoryExportsHandler = routes.NewMemoryExportsHandler(db, themoryClient, logger, exportDir, workerDelay)
	} else {
		logger.Warn("THEMORY_BASE_URL not set — /v1/memory/* endpoints disabled")
	}

	// Memory Webhook Delivery plan T5.5 — background dispatcher worker.
	// Only started when both memory proxy AND DB are configured (worker
	// needs both to poll + dispatch).
	var memoryWebhookDeliveriesHandler *routes.MemoryWebhookDeliveriesHandler
	if memoryHandler != nil && db != nil {
		memoryWebhookDeliveriesHandler = routes.NewMemoryWebhookDeliveriesHandler(db, logger)
	}

	// Router
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)

	// OTel HTTP tracing middleware -- creates a span per request with route info.
	r.Use(func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "cloud-service",
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				return r.Method + " " + r.URL.Path
			}),
		)
	})

	// MEET-040: CORS removed — dashboard is same-origin (cloud.{domain}).
	// All API calls are relative URLs, no cross-origin requests.

	// Request logging — every request gets logged with trace_id for correlation.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" { // Don't log health checks
				attrs := []any{
					"method", r.Method,
					"path", r.URL.Path,
					"remote", r.RemoteAddr,
				}
				sc := trace.SpanContextFromContext(r.Context())
				if sc.IsValid() {
					attrs = append(attrs, "trace_id", sc.TraceID().String())
				}
				logger.Info("request", attrs...)
			}
			next.ServeHTTP(w, r)
		})
	})

	// Health check — reports DB connectivity for operational visibility
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		dbStatus := "not_configured"
		if db != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := db.PingContext(ctx); err != nil {
				dbStatus = "disconnected"
			} else {
				dbStatus = "connected"
			}
		}

		status := "ok"
		if db != nil && dbStatus != "connected" {
			status = "degraded"
		}

		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": status,
			"db":     dbStatus,
		})
	})

	// §4.1+4.2: Metrics endpoint (Prometheus text format)
	r.Get("/metrics", cloudmetrics.Handler())

	// Auth endpoints (no auth — these ARE the auth flow)
	r.Route("/v1/auth", func(r chi.Router) {
		// Device endpoints are unauthenticated — rate-limit per client IP to
		// blunt brute force / abuse (find_sec_007).
		r.With(ratelimit.Middleware(deviceLimiter, 60, ipKey)).Post("/device/authorize", deviceHandler.Authorize)
		r.With(ratelimit.Middleware(deviceLimiter, 60, ipKey)).Post("/device/accept", deviceHandler.Accept)
		r.With(ratelimit.Middleware(deviceLimiter, 60, ipKey)).Post("/device/token", deviceHandler.Token)
		r.With(ratelimit.Middleware(deviceLimiter, 60, ipKey)).Post("/token/refresh", deviceHandler.Refresh)
		r.Post("/session/token", sessionTokenHandler.Exchange) // Dashboard: Kratos session → JWT (rate-limited inside the handler)

		// /me requires JWT
		r.With(requireJWT).Get("/me", meHandler.Get)
	})

	// Telemetry — T1.2 of plan 2026-05-27-backend-frontend-integration v1.1.
	// PUBLIC (no JWT) — sendBeacon API cannot set custom Authorization
	// headers, so requiring auth would silently drop every beacon. Body cap
	// 64 KiB (EC-03) defends against OOM via hostile / buggy clients.
	telemetryHandler := routes.NewTelemetryHandler(logger)
	r.Route("/v1/telemetry", func(r chi.Router) {
		r.Post("/errors", telemetryHandler.Errors)
		r.Post("/web-vitals", telemetryHandler.WebVitals)
		r.Post("/events", telemetryHandler.Events)
		r.Post("/interactions", telemetryHandler.Interactions)
	})

	// Hydra consent/login callbacks (browser redirects)
	r.Get("/auth/login", consentHandler.Login)
	r.Get("/auth/login/complete", consentHandler.LoginComplete)
	r.Get("/auth/consent", consentHandler.Consent)
	r.Get("/auth/device", consentHandler.DeviceVerify)

	// J-09 T1.1 — GitHub App install handshake (browser redirects).
	// /v1/integrations/github/install sets a CSRF state cookie + 302s to GitHub.
	// /v1/integrations/github/callback validates the state + bounces back to
	// the dashboard, preserving installation_id so the next POST
	// /api/v1/projects/{name}/connect can carry it through.
	loginURL := cfg.AppPublicURL + "/login"
	if cfg.KratosPublicURL != "" {
		loginURL = cfg.KratosPublicURL + "/login"
	}
	ghIntegrations := routes.NewGitHubIntegrationsHandler(
		cfg.GitHubAppSlug,
		cfg.AppPublicURL,
		loginURL,
		cfg.AuthCookieDomain,
		func(req *http.Request) bool {
			// EC-15: cheap session check — we only need a valid Kratos
			// browser session cookie to know the user can complete the
			// connect flow without re-login. Token expiry is handled
			// upstream by /v1/auth/session/token.
			return req.Header.Get("Cookie") != ""
		},
	)
	r.Get("/v1/integrations/github/install", ghIntegrations.InitiateInstall)
	r.Get("/v1/integrations/github/callback", ghIntegrations.Callback)

	// Plan 0 — Platform API Keys (/v1/api-keys/*).
	// D8 dispatch lockdown: this route group uses requireJWT (session cookie
	// from Ory Hydra OIDC). It does NOT use RequireScope — that middleware
	// gates SDK-consumer routes (/v1/memory/* in Plan 1, future verticals
	// thereafter). Mixing the two on the same route would either create a
	// bootstrap loop (Bearer key needs CRUD via Bearer key) or leak the key-
	// management surface to production agents.
	r.Route("/v1/api-keys", func(r chi.Router) {
		r.Use(requireJWT)
		// Per-user rate limit reuses the billingLimiter pool (30 rpm).
		// API key CRUD is a low-volume operation by definition.
		r.Use(ratelimit.Middleware(billingLimiter, 30, userKey))
		r.Get("/list", apiKeysHandler.ListKeys)
		r.Post("/", apiKeysHandler.CreateKey)
		r.Delete("/{id}", apiKeysHandler.RevokeKey)
		// API Key Rotation plan T6.3 (closes Plan 0 D6 deferral).
		r.Post("/{id}/rotate", apiKeysHandler.RotateKey)
		// API Key Lineage View plan T7.3 (closes API Key Rotation OOS #4).
		r.Get("/{id}/lineage", apiKeysHandler.LineageKey)
	})

	// Plan 1 — Memory Shell (/v1/memory/*).
	// D8 dispatch + D2 scope mapping: each route uses RequireScope(db, scope)
	// — the SDK-consumer surface authenticated by loopzero_live_* Bearer keys.
	// /v1/api-keys/* (above) and /v1/memory/* (here) intentionally use
	// distinct middleware so a customer can never CRUD keys via Bearer or
	// query memory via session cookie (closes both directions of the loop).
	if memoryHandler != nil {
		r.Route("/v1/memory", func(r chi.Router) {
			r.With(auth.RequireScope(db, "memory:read")).Get("/list", memoryHandler.List)
			r.With(auth.RequireScope(db, "memory:read")).Get("/metrics", memoryHandler.Metrics)
			r.With(auth.RequireScope(db, "memory:delete")).Delete("/forget/{memoryID}", memoryHandler.Forget)
			// Plan 2 (Memory Insights) — entities aggregated by scope.
			// Same scope as List + Metrics per D2 (no per-feature scope explosion).
			r.With(auth.RequireScope(db, "memory:read")).Get("/entities", memoryHandler.Entities)

			// Plan 3 (Memory Operations) — webhooks + exports.
			// All gated by memory:admin per least-privilege (these affect
			// downstream consumers + can exfiltrate data).
			if memoryWebhooksHandler != nil {
				r.With(auth.RequireScope(db, "memory:admin")).Post("/webhooks", memoryWebhooksHandler.Create)
				r.With(auth.RequireScope(db, "memory:admin")).Get("/webhooks/list", memoryWebhooksHandler.List)
				r.With(auth.RequireScope(db, "memory:admin")).Delete("/webhooks/{id}", memoryWebhooksHandler.Delete)
			}
			if memoryExportsHandler != nil {
				r.With(auth.RequireScope(db, "memory:admin")).Post("/exports", memoryExportsHandler.Create)
				r.With(auth.RequireScope(db, "memory:admin")).Get("/exports/{id}/status", memoryExportsHandler.Status)
				r.With(auth.RequireScope(db, "memory:admin")).Get("/exports/{id}/download", memoryExportsHandler.Download)
			}

			// Memory Webhook Delivery plan T5.7 — deliveries history per webhook.
			if memoryWebhookDeliveriesHandler != nil {
				r.With(auth.RequireScope(db, "memory:admin")).Get("/webhooks/{id}/deliveries", memoryWebhookDeliveriesHandler.List)
				// Delivery Replay plan T8.3 — manual re-trigger for a failed delivery.
				// Distinct path-param names ({webhook_id}, {delivery_id}) match the
				// handler's chi.URLParam reads.
				r.With(auth.RequireScope(db, "memory:admin")).Post(
					"/webhooks/{webhook_id}/deliveries/{delivery_id}/replay",
					memoryWebhookDeliveriesHandler.Replay,
				)
			}
		})
	}

	// Memory Webhook Delivery plan T5.5: the dispatcher worker is started
	// below alongside the metering scheduler, sharing the schedulerCtx for
	// graceful shutdown. See the `if db != nil && stripeClient != nil`
	// block + adjacent worker spawn.

	// Billing endpoints (JWT required via middleware, except webhook)
	if billingHandler != nil {
		r.Route("/v1/billing", func(r chi.Router) {
			r.Use(requireJWT)
			// Per-user rate limit on billing endpoints (find_sec_007).
			r.Use(ratelimit.Middleware(billingLimiter, 30, userKey))
			// Reading billing status is allowed for any member of the team.
			r.Get("/", billingHandler.GetBilling)
			// Mutating billing (opening the Stripe portal, starting checkout) is
			// owner-only. Role is resolved from the DB (S2.3), never the token.
			r.With(auth.RequireRole(tenantProvisioner, "owner")).Get("/portal", billingHandler.Portal)
			r.With(auth.RequireRole(tenantProvisioner, "owner")).Post("/checkout", billingHandler.Checkout)
		})
		// Webhook — NO JWT (Stripe signs with its own secret)
		r.Post("/v1/webhooks/stripe", billingHandler.Webhook)
	}

	// Engine API proxy — dashboard calls engine through cloud service.
	// The engine only accepts RSA tokens (aud=loopzero-engine) or internal service auth.
	// Dashboard has HMAC tokens, so cloud authenticates to engine via X-Service-Auth.
	engineURL := cfg.EngineAPIURL
	serviceSecret := string(cfg.ServiceAuthSecret)

	// Dual-control Ed25519 signer (ADR 2026-05-13). The cloud proxy
	// signs the canonical payload (user, tenant, ts, nonce, method+path)
	// in addition to sending the shared secret. Phase 1: signer is
	// optional — if LOOPZERO_SERVICE_AUTH_ED25519_PRIVATE_KEY is empty, the
	// proxy degrades gracefully to single-control. The engine middleware
	// counts present-vs-absent and the operator flips to Phase 2 when
	// the 7-day coverage soak hits 100% (RFC Q3).
	var serviceSigner *auth.ServiceSigner
	if ed25519Key := os.Getenv("LOOPZERO_SERVICE_AUTH_ED25519_PRIVATE_KEY"); ed25519Key != "" {
		serviceSigner, err = auth.NewServiceSignerFromBase64(ed25519Key)
		if err != nil {
			// Per RFC Q5 fail-closed: a misconfigured key MUST NOT silently
			// degrade to single-control. Crash at startup so the readiness
			// probe stops the rollout instead of accepting traffic without
			// the dual-control gate.
			logger.Error("service-auth Ed25519 signer init failed", "error", err)
			os.Exit(1)
		}
		logger.Info("service-auth Ed25519 dual-control signer enabled")
	} else {
		logger.Warn("service-auth Ed25519 private key not configured — Phase 1 single-control only; set LOOPZERO_SERVICE_AUTH_ED25519_PRIVATE_KEY to enable dual-control")
	}

	engineClient := &http.Client{
		Timeout:   30 * time.Second,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	r.Route("/engine", func(r chi.Router) {
		r.Use(requireJWT) // Dashboard must be authenticated
		r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
			// Strip /engine prefix and forward to engine API
			targetPath := strings.TrimPrefix(r.URL.Path, "/engine")
			if targetPath == "" {
				targetPath = "/"
			}

			// #nosec G704 -- authenticated reverse proxy (requireJWT); host is the operator-configured EngineAPIURL, only the path is forwarded by design.
			proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, engineURL+targetPath, r.Body)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(w).Encode(map[string]string{"code": "SERVICE_ERROR", "message": "Service temporarily unavailable. Try again."})
				return
			}
			// Forward query params, content type, and correlation ID
			proxyReq.URL.RawQuery = r.URL.RawQuery
			proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))
			if cid := r.Header.Get("X-Correlation-Id"); cid != "" {
				proxyReq.Header.Set("X-Correlation-Id", cid)
			}
			if rid := r.Header.Get("X-Request-Id"); rid != "" {
				proxyReq.Header.Set("X-Request-Id", rid)
			}

			// Service-to-service auth: cloud authenticates to engine via shared secret.
			// S2.3: Proxy sends identity only (user ID, tenant, email). Engine resolves
			// role from DB — never trusts role from headers.
			proxyReq.Header.Set("X-Service-Auth", serviceSecret)
			user := auth.GetUser(r.Context())
			if user != nil {
				proxyReq.Header.Set("X-LoopZero-User-ID", user.Subject)
				proxyReq.Header.Set("X-LoopZero-Tenant", user.TenantID)
				proxyReq.Header.Set("X-LoopZero-Email", user.Email)
				// X-LoopZero-Role intentionally NOT sent — engine resolves from DB (S2.3)

				// Dual-control Ed25519 (ADR 2026-05-13). Attach AFTER identity
				// headers are set — Attach hashes user+tenant+method+path into
				// the canonical payload. A signing failure (missing identity,
				// nonce-rng exhaustion) returns 502 to dashboard caller rather
				// than silently proceeding without the signature.
				if serviceSigner != nil {
					if sErr := serviceSigner.Attach(proxyReq); sErr != nil {
						logger.Warn("service-auth signature attach failed", "error", sErr, "path", targetPath)
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusBadGateway)
						json.NewEncoder(w).Encode(map[string]string{"code": "SERVICE_AUTH_SIGN_FAILED", "message": "Service authentication failure. Try again."})
						return
					}
				}
			}

			// #nosec G704 -- authenticated reverse proxy (requireJWT); destination host is the operator-configured EngineAPIURL, not attacker-controlled.
			resp, err := engineClient.Do(proxyReq)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(w).Encode(map[string]string{"code": "SERVICE_UNAVAILABLE", "message": "Service temporarily unavailable. Try again."})
				return
			}
			defer func() { _ = resp.Body.Close() }()

			// Copy response headers and body
			for k, v := range resp.Header {
				for _, vv := range v {
					w.Header().Add(k, vv)
				}
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
		})
	})

	// Start metering scheduler (background goroutines)
	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	if db != nil && stripeClient != nil {
		reporter := metering.NewReporter(db, stripeClient, logger)
		reconciler := metering.NewReconciler(db, logger)
		go metering.StartScheduler(schedulerCtx, reporter, reconciler, logger)
	}

	// Memory Webhook Delivery plan T5.5 — dispatcher worker goroutine.
	// Sharing schedulerCtx ensures graceful shutdown on SIGTERM. The
	// worker drains in-flight dispatches before exiting Run().
	if memoryHandler != nil && db != nil {
		webhookWorker := memory_webhook_worker.New(db, logger, nil)
		go webhookWorker.Run(schedulerCtx)
		logger.Info("memory webhook dispatcher started")
	}

	// T1.9 production deployment: start cost aggregator if Prometheus
	// is configured. Opt-in via LOOPZERO_PROMETHEUS_URL — when unset, the
	// aggregator stays disabled and cost_counters remain empty (the API
	// middleware fails-open). Interval defaults to 5 min, overridable
	// via LOOPZERO_COST_AGGREGATOR_INTERVAL.
	if db != nil {
		if promURL := strings.TrimSpace(os.Getenv("LOOPZERO_PROMETHEUS_URL")); promURL != "" {
			prom, perr := billing.NewHTTPPromQuerier(promURL, logger)
			if perr != nil {
				logger.Error("cost aggregator: prometheus client init failed; aggregator disabled",
					"error", perr,
					"prometheus_url", promURL)
			} else {
				interval := 5 * time.Minute
				if iv := os.Getenv("LOOPZERO_COST_AGGREGATOR_INTERVAL"); iv != "" {
					if parsed, perr := time.ParseDuration(iv); perr == nil && parsed > 0 {
						interval = parsed
					}
				}
				aggregator := billing.NewCostAggregator(db, prom, logger)
				go aggregator.Run(schedulerCtx, interval)
				logger.Info("cost aggregator started",
					"prometheus_url", promURL,
					"interval", interval.String())
			}
		} else {
			logger.Info("cost aggregator disabled (LOOPZERO_PROMETHEUS_URL not set)")
		}
	}

	// Start server
	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Info("shutting down cloud service")
		schedulerCancel() // Stop background metering jobs
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		if tracingShutdown != nil {
			if err := tracingShutdown(ctx); err != nil {
				logger.Warn("tracing shutdown error", "error", err)
			}
		}
		if db != nil {
			_ = db.Close()
		}
	}()

	logger.Info("cloud auth service starting",
		"addr", cfg.ListenAddr,
		"hydra_public", cfg.HydraPublicURL,
		"kratos_public", cfg.KratosPublicURL,
		"client_id", cfg.HydraClientID,
	)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
