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
	_ "github.com/lib/pq"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"

	loopzerojwt "github.com/loopzero/loopzero/pkg/jwt"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	"github.com/loopzero/loopzero/cloud/internal/billing"
	"github.com/loopzero/loopzero/cloud/internal/config"
	"github.com/loopzero/loopzero/cloud/internal/consent"
	"github.com/loopzero/loopzero/cloud/internal/metering"
	cloudmetrics "github.com/loopzero/loopzero/cloud/internal/metrics"
	"github.com/loopzero/loopzero/cloud/internal/routes"
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
		db, err = sql.Open("postgres", cfg.DatabaseURL)
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

	// Handlers
	deviceHandler := routes.NewDeviceHandler(hydra, cfg.BillingReturnURL)
	meHandler := routes.NewMeHandler()
	consentHandler := consent.NewHandler(hydra, tenantProvisioner, cfg.KratosPublicURL, cfg.KratosInternalURL, logger)

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

		json.NewEncoder(w).Encode(map[string]string{
			"status": status,
			"db":     dbStatus,
		})
	})

	// §4.1+4.2: Metrics endpoint (Prometheus text format)
	r.Get("/metrics", cloudmetrics.Handler())

	// Auth endpoints (no auth — these ARE the auth flow)
	r.Route("/v1/auth", func(r chi.Router) {
		r.Post("/device/authorize", deviceHandler.Authorize)
		r.Post("/device/token", deviceHandler.Token)
		r.Post("/token/refresh", deviceHandler.Refresh)
		r.Post("/session/token", sessionTokenHandler.Exchange) // Dashboard: Kratos session → JWT

		// /me requires JWT
		r.With(requireJWT).Get("/me", meHandler.Get)
	})

	// Hydra consent/login callbacks (browser redirects)
	r.Get("/auth/login", consentHandler.Login)
	r.Get("/auth/login/complete", consentHandler.LoginComplete)
	r.Get("/auth/consent", consentHandler.Consent)
	r.Get("/auth/device", consentHandler.DeviceVerify)

	// Billing endpoints (JWT required via middleware, except webhook)
	if billingHandler != nil {
		r.Route("/v1/billing", func(r chi.Router) {
			r.Use(requireJWT)
			r.Get("/", billingHandler.GetBilling)
			r.Get("/portal", billingHandler.Portal)
			r.Post("/checkout", billingHandler.Checkout)
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

			proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, engineURL+targetPath, r.Body)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadGateway)
				json.NewEncoder(w).Encode(map[string]string{"code": "SERVICE_ERROR", "message": "Service temporarily unavailable. Try again."})
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

			resp, err := engineClient.Do(proxyReq)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadGateway)
				json.NewEncoder(w).Encode(map[string]string{"code": "SERVICE_UNAVAILABLE", "message": "Service temporarily unavailable. Try again."})
				return
			}
			defer resp.Body.Close()

			// Copy response headers and body
			for k, v := range resp.Header {
				for _, vv := range v {
					w.Header().Add(k, vv)
				}
			}
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
		})
	})

	// Start metering scheduler (background goroutines)
	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	if db != nil && stripeClient != nil {
		reporter := metering.NewReporter(db, stripeClient, logger)
		reconciler := metering.NewReconciler(db, logger)
		go metering.StartScheduler(schedulerCtx, reporter, reconciler, logger)
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
		srv.Shutdown(ctx)
		if tracingShutdown != nil {
			if err := tracingShutdown(ctx); err != nil {
				logger.Warn("tracing shutdown error", "error", err)
			}
		}
		if db != nil {
			db.Close()
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
