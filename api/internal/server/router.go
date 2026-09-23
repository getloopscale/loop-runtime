// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package server assembles the HTTP router (chi) + middleware
// pipeline + dependency wiring for the API binary. Routes are
// declared per resource group (tenant, project, build, release,
// etc.) and dispatched to services in `api/internal/services/`.
// All mutating routes pass through `withEnvironmentLock` per
// CLAUDE.md Rule 6.
package server

import (
	"context"
	"net/http"

	"github.com/getloopscale/loopzero/api/internal/routes/admin"
	"github.com/getloopscale/loopzero/api/internal/routes/auth"
	"github.com/getloopscale/loopzero/api/internal/routes/projects"
	"github.com/getloopscale/loopzero/api/internal/routes/tenants"
	"github.com/getloopscale/loopzero/api/internal/routes/timeline"
	"github.com/getloopscale/loopzero/api/internal/routes/version"

	"github.com/go-chi/chi/v5"

	planpkg "github.com/getloopscale/loopzero-contracts/plan"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	audit "github.com/getloopscale/loopzero/api/internal/middleware/audit"
	mwauth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	observability "github.com/getloopscale/loopzero/api/internal/middleware/observability"
	policy "github.com/getloopscale/loopzero/api/internal/middleware/policy"
	"github.com/getloopscale/loopzero/api/internal/routes/bootstrap"
	"github.com/getloopscale/loopzero/api/internal/routes/health"
	"github.com/getloopscale/loopzero/api/internal/routes/secrets"
	"github.com/getloopscale/loopzero/api/internal/routes/source"
	"github.com/getloopscale/loopzero/api/internal/sse"
	"github.com/getloopscale/loopzero/api/internal/surfaces"
	"github.com/getloopscale/loopzero/pkg/enforcement"
)

// enforcementMetricBuildMinutes is the canonical metric label for the cost
// cap gating the POST /api/v1/builds route. Distinct from PlanLimiter's
// "builds_per_month" — see Wave 1 T1.9 for the SRP rationale.
const enforcementMetricBuildMinutes = planpkg.MetricBuildMinutes

// NewRouter creates a chi router with middleware and routes.
func NewRouter(cfg Config) *chi.Mux {
	r := chi.NewRouter()

	// Pre-auth IP rate limit (T2.4 of deep-review-remediation-plan, ADR D4).
	// Mounted FIRST so brute-force attempts on /api/v1/auth/* — or any other
	// route — are rejected at the edge without consuming downstream resources.
	// Trusted proxy CIDRs from env so production CF Tunnel can be honored
	// while dev k3d defaults to "trust nobody".
	if cfg.IPRateLimit != nil {
		r.Use(cfg.IPRateLimit.Middleware)
	}

	// Security headers — before CORS so they are always present.
	r.Use(policy.SecurityHeaders)

	// CORS — allow dashboard (cloud.{domain}) to call API (api.{domain}).
	// Must be early middleware so preflight OPTIONS get handled before auth.
	r.Use(corsMiddleware(cfg.CORSAllowedOrigin, cfg.IsProduction))

	// Maintenance mode: block mutating requests during infrastructure operations
	// (e.g., PG resize). Must come before auth so 503 is returned without token check.
	r.Use(policy.MaintenanceMode)

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
	if cfg.PGPoolGate != nil {
		pgCB := policy.NewPGCircuitBreaker(policy.PGCircuitBreakerConfig{
			Gate: cfg.PGPoolGate,
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
	health := health.NewHealthHandler(cfg.DB, cfg.HealthServices...)
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
	r.Get("/api/v1/version", version.VersionHandler())
	// Contrato da FROTA de status: o coletor do dashboard sonda `/v1/version` e
	// le' `{version, built_at}`. Mesma verdade do endpoint acima, no formato que
	// os outros servicos da plataforma ja' falam — sem o qual o engine nao
	// aparece no painel /status. Publico pelo mesmo motivo: metadado de build nao
	// e' segredo e o coletor sonda sem sessao.
	r.Get("/v1/version", version.FleetVersionHandler())

	// Internal-only routes (no auth — called by platform controllers, not users)
	if surfaces.Recorded(cfg.Surfaces, surfaces.Bootstrap, cfg.BootstrapStore != nil,
		"sem banco — o BootstrapStore é `PostgresBootstrapStore` (setup_stores.go:117) "+
			"e só é construído dentro do bloco que exige `dbAdapter`") {
		bootstrap := bootstrap.NewBootstrapHandler(cfg.BootstrapStore, cfg.BootstrapSecret, cfg.CellPlacement)
		r.Post("/internal/bootstrap", bootstrap.Bootstrap)
	}
	if surfaces.Recorded(cfg.Surfaces, surfaces.ArgoCallback, cfg.ArgoCallbackHandler != nil,
		"as dependências do orquestrador não subiram — o handler vem de `orchDeps` "+
			"(setup_server.go:236), que é nil quando a orquestração de build não foi montada") {
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
	if surfaces.Recorded(cfg.Surfaces, surfaces.SecretFetch, cfg.SecretFetchHandler != nil,
		"falta uma de quatro dependências, e main.go:429-441 exige as quatro: transit "+
			"do OpenBao, banco, cellStore e `LOOPZERO_BROKER_TOKEN`. Medido em dev-public "+
			"2026-08-16: o Secret `loopzero-broker-token` NÃO EXISTE, então esta é a "+
			"superfície que já está desligada agora") {
		// T2.1 + ADR D2 of plan 2026-06-10-j22-layer2-code-review-fixes
		// (revised 2026-06-11 LIVE catch — ESO v1beta1 CRD rejects
		// `remoteRef.metadata` as unknown field): 2-segment URL where
		// {secret_name} is a composite `secretName.projectID.envName`
		// (EC-08 regex `^[A-Z][A-Z0-9_]*$` forbids `.` → safe delimiter).
		// Handler splits property into 3 parts and scopes broker SQL by
		// canonical composite unique key (closes HIGH #15 scope-drop).
		r.Post("/internal/v1/secret-fetch/{tenant_id}/{secret_name}", cfg.SecretFetchHandler)
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
			"GITHUB_WEBHOOK_SECRET ausente — o segredo HMAC que valida as entregas do GitHub")
	}

	// Admin endpoints — operator primitives, X-Service-Auth only (no user
	// context). Sibling of /webhooks/github so it bypasses the user-auth
	// middleware that requires identity headers.
	//
	// qa-sprint2-cleanup-2026-06-24 T1.2 (closes loopzero-cloud#16): /evict now
	// wraps the handler with mwauth.RequireAdminService (chi canonical
	// r.With form). The middleware reads only LOOPZERO_ADMIN_SERVICE_AUTH_SECRET
	// (canonical) + _PREVIOUS — NOT the legacy LOOPZERO_SERVICE_AUTH_SECRET that
	// the cloud /engine/* proxy carries on user-JWT requests. The handler
	// still enforces LOOPZERO_ADMIN_API=true as a separate defense-in-depth gate.
	if surfaces.Recorded(cfg.Surfaces, surfaces.AdminEvict, cfg.AdminEvictor != nil,
		"sem banco — `buildAdminEvictor` (setup_server.go:290) roda dentro do bloco "+
			"que exige `dbAdapter`. A ausência do Secret `loopzero-api-admin` NÃO desliga "+
			"esta rota: ele alimenta a chave que o middleware confere, e sem ela o "+
			"acesso é NEGADO (falha fechada), não aberto") {
		r.With(mwauth.RequireAdminService).Post(
			"/api/v1/admin/tenants/evict",
			admin.AdminEvictHandler(cfg.AdminEvictor),
		)
	}
	// CHG-2026-04-30-DELETE-SINGLE-CELL: pin tenants to specific cell.
	if surfaces.Recorded(cfg.Surfaces, surfaces.AdminPin, cfg.AdminPinner != nil,
		"sem banco — o CellPinner é construído sobre `PostgresCellStore` "+
			"(setup_server.go:299). Esta é a ÚNICA rota que move um tenant de célula, "+
			"então desligada ela não degrada: ela remove a operação") {
		r.Post("/api/v1/admin/tenants/pin", admin.AdminPinCellHandler(cfg.AdminPinner, cfg.AuditWriter))
	}
	// T4.4 of plan 2026-05-28-meet-001-engineering-closure-plan.md:
	// diagnostic read-only endpoint for on-call to inspect a release's
	// signing + attestation chain without source access. The handler
	// enforces LOOPZERO_ADMIN_API + X-Service-Auth itself.
	if surfaces.Recorded(cfg.Surfaces, surfaces.ReleaseInspect, cfg.ReleaseInspector != nil,
		"sem banco — construído em setup_server.go:315, dentro do bloco que exige "+
			"`dbAdapter`. É a visão de plantão sobre a cadeia de assinatura de uma "+
			"release; desligada, o plantão perde o diagnóstico exatamente quando precisa") {
		r.Get("/api/v1/admin/releases/{id}/inspect", admin.AdminReleaseInspectHandler(cfg.ReleaseInspector))
	}

	// Protected routes (auth required)
	//
	// A superfície de maior alcance do roteador: desligada, TODA rota de cliente
	// desaparece de uma vez. Medido: `TokenValidator` é construído
	// incondicionalmente (setup_auth.go:75), entao em producao a guarda depende
	// so' do `MemberStore` — a condicao existe para os testes poderem montar um
	// roteador sem auth.
	if surfaces.Recorded(cfg.Surfaces, surfaces.AuthProtected,
		cfg.TokenValidator != nil && cfg.MemberStore != nil,
		"sem validador de token ou sem MemberStore. Em producao o validador e' "+
			"sempre construido (setup_auth.go:75), logo na pratica isto significa "+
			"MemberStore ausente — que depende do banco") {
		r.Group(func(r chi.Router) {
			r.Use(mwauth.Auth(cfg.TokenValidator, cfg.MemberStore))
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
			auth := auth.NewAuthHandler(cfg.AuthUserLookup).
				WithMembershipLookup(cfg.MemberStore)
			if cfg.ActiveTeamStore != nil {
				auth = auth.WithActiveTeamStore(cfg.ActiveTeamStore)
			}
			r.Get("/api/v1/auth/whoami", auth.Whoami)

			// Rate limiting middleware chain — used for all mutation routes.
			rateLimitMiddlewares := make([]func(http.Handler) http.Handler, 0, 1)
			if cfg.RateLimiter != nil {
				rateLimitMiddlewares = append(rateLimitMiddlewares, cfg.RateLimiter.Middleware())
			}

			// Plan T2.5 D8 full — POST /team/switch with rate limiting.
			if surfaces.Recorded(cfg.Surfaces, surfaces.ActiveTeam, cfg.ActiveTeamStore != nil,
				"sem banco — `NewPostgresActiveTeamStore` (setup_server.go:288) roda "+
					"dentro do bloco que exige `dbAdapter`. Desligada, /whoami cai na "+
					"heurística do middleware e a troca de equipe some") {
				r.With(rateLimitMiddlewares...).Post("/api/v1/auth/team/switch", auth.SwitchTeam)
			}

			// Idempotent mutation middleware chain — rate limiting + idempotency.
			idempotentMiddlewares := make([]func(http.Handler) http.Handler, 0, 2)
			idempotentMiddlewares = append(idempotentMiddlewares, rateLimitMiddlewares...)
			if cfg.IdempotencyStore != nil {
				idempotentMiddlewares = append(idempotentMiddlewares, policy.Idempotency(cfg.IdempotencyStore))
			}

			// Plan enforcement middlewares (Phase 1).
			tenantFromCtx := func(r *http.Request) string {
				uc := mwauth.GetUserContext(r.Context())
				if uc == nil {
					return ""
				}
				return uc.TenantID
			}
			upgradeURL := cfg.UpgradeURL
			if upgradeURL == "" {
				upgradeURL = "/billing"
			}
			usage := cfg.planUsage()

			// M4 (account-level tenancy, Phase 2): the per-TENANT projects 402
			// middleware was REMOVED here. The project cap is now enforced
			// per-Account at the single creation path (PostgresProjectStore,
			// atomic advisory-lock count+insert) with the canonical 422
			// PROJECTS_EXCEED_PLAN — see cmd/setup_database.go SetProjectCapChecker.
			buildEnforcement := enforcement.Middleware(cfg.PlanLimiter, enforcement.OperationCheck{
				Metric:  "builds_per_month",
				CountFn: usage.BuildsThisMonth,
			}, tenantFromCtx, upgradeURL)

			// T1.9 (Wave 1): cost cap gates the build route on build_minutes.
			// Nil-safe: when CostEnforcer is unset (boot before wire, or
			// missing cost_counters table), Middleware is pass-through.
			// Distinct from buildEnforcement (rate-style builds/month).
			buildCostGate := cfg.CostEnforcer.Middleware(enforcementMetricBuildMinutes)

			// D5 of plan 2026-06-07-preview-as-category-single-slot-plan.md
			// (Bug A fix): PermanentEnvironments counts only
			// `kind = 'permanent'` rows, so previews (single-slot model via
			// preview_slot table) NEVER count against plan_limits.environments.
			envEnforcement := enforcement.Middleware(cfg.PlanLimiter, enforcement.OperationCheck{
				Metric:  "environments",
				CountFn: usage.PermanentEnvironments,
			}, tenantFromCtx, upgradeURL)

			seatEnforcement := enforcement.Middleware(cfg.PlanLimiter, enforcement.OperationCheck{
				Metric:  "seats",
				CountFn: usage.Seats,
			}, tenantFromCtx, upgradeURL)

			registerProtectedRoutes(r, cfg, rateLimitMiddlewares, idempotentMiddlewares,
				buildEnforcement, envEnforcement, seatEnforcement, buildCostGate)
		})
	}

	// C-01 fix: Callback route removed. Build completion is now detected by the
	// BuildReconcileLoop which polls Argo Workflow CRDs from the control plane.

	return r
}

// registerProtectedRoutes wires all authenticated API routes.
func registerProtectedRoutes(
	r chi.Router,
	cfg Config,
	rateLimitMiddlewares []func(http.Handler) http.Handler,
	idempotentMiddlewares []func(http.Handler) http.Handler,
	buildEnforcement, envEnforcement, seatEnforcement func(http.Handler) http.Handler,
	buildCostGate func(http.Handler) http.Handler,
) {
	// Source upload — auth + rate limiting, no idempotency.
	if surfaces.Recorded(cfg.Surfaces, surfaces.SourceUpload, cfg.SourceStore != nil,
		"sem banco — o SourceStore vem de `stores` (setup_server.go), que exige `dbAdapter`. "+
			"Desligada, o primeiro passo de todo deploy — subir o pacote — desaparece") {
		var sourceOpts []func(*source.SourceHandler)
		if cfg.ObjectUploader != nil {
			sourceOpts = append(sourceOpts, source.WithObjectUploader(cfg.ObjectUploader))
		}
		// CHG-2026-04-29-DOGFOOD iter 9 — WithProductionMode removed:
		// upload errors are now always fatal when an object store is
		// configured. Local must equal production (Rule 10).
		if cfg.SourceBucket != "" {
			sourceOpts = append(sourceOpts, source.WithSourceBucket(cfg.SourceBucket))
		}
		sourceHandler := source.NewSourceHandler(cfg.SourceStore, sourceOpts...)
		r.With(rateLimitMiddlewares...).Post("/api/v1/source/upload", sourceHandler.Upload)
	}

	// Project routes
	if surfaces.Recorded(cfg.Surfaces, surfaces.Projects,
		cfg.ProjectStore != nil && cfg.TenantCRMgr != nil,
		"sem banco ou sem acesso ao cluster — o ProjectStore vem de `dbDeps` e o "+
			"TenantCRMgr do cliente de Kubernetes; a rota exige os dois") {
		projects := projects.NewProjectHandler(cfg.ProjectStore, cfg.TenantCRMgr)
		if cfg.ProjectDeleter != nil {
			projects.SetDeleter(cfg.ProjectDeleter)
		}
		// QA Run-3 #123: wire canonical tier resolver so Create can
		// enforce plan caps and downstream Scale/Project surfaces see
		// the right tier. Without this, scale.go:252-254 falls back
		// to PlanTierStarter silently even when subscriptions.plan
		// = 'team' active in DB.
		if cfg.PlanTierStore != nil {
			projects.SetPlanTierStore(cfg.PlanTierStore)
		}
		// T4.1 of plan 2026-06-09-m2-operator-canonical-fix v1.4: auto-create
		// `production` env on POST /api/v1/projects (ADR D4 — Vercel-style).
		// nil-safe: when ProjectEnvEnsurer not wired, Create behaves
		// identically to pre-T4.1 (TestProjectCreate_BackwardCompatNoEnsurer
		// pins this contract).
		if cfg.ProjectEnvEnsurer != nil {
			projects.SetEnvEnsurer(cfg.ProjectEnvEnsurer)
		}
		r.Get("/api/v1/projects", projects.List)
		r.Get("/api/v1/projects/{name}", projects.Get)
		r.With(rateLimitMiddlewares...).Post("/api/v1/projects", projects.Create)
		r.With(rateLimitMiddlewares...).Delete("/api/v1/projects/{name}", projects.Delete)
		r.With(rateLimitMiddlewares...).Delete("/api/v1/projects/{name}/environments/{env}", projects.DeleteEnvironment)
	}

	// SSE stream route (read-only)
	if surfaces.Recorded(cfg.Surfaces, surfaces.EventsStream,
		cfg.EventStore != nil && cfg.BuildStoreReader != nil,
		"sem banco — ambos vêm de `stores`. Desligada, o CLI perde o acompanhamento "+
			"do deploy e fica sem saber se ele avançou") {
		sseOpts := []sse.StreamOption{sse.WithLogger(cfg.Logger)}
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

		// Plan J-17 T2.3 — project-level SSE stream. Reuses the same poll /
		// heartbeat / concurrency knobs as the build-level stream so operators
		// configure one set of intervals. EventStore tenant isolation is the
		// canonical authorization.
		projectStreamOpts := []sse.ProjectStreamOption{sse.WithProjectLogger(cfg.Logger)}
		if cfg.SSEHeartbeatInterval > 0 {
			projectStreamOpts = append(projectStreamOpts, sse.WithProjectHeartbeatInterval(cfg.SSEHeartbeatInterval))
		}
		if cfg.SSEPollInterval > 0 {
			projectStreamOpts = append(projectStreamOpts, sse.WithProjectPollInterval(cfg.SSEPollInterval))
		}
		if cfg.SSEMaxConcurrentStreams > 0 {
			projectStreamOpts = append(projectStreamOpts, sse.WithProjectMaxConcurrentStreams(cfg.SSEMaxConcurrentStreams))
		}
		projectStreamHandler := sse.NewProjectStreamHandler(cfg.EventStore, projectStreamOpts...)
		r.Get("/api/v1/projects/{name}/builds/stream", projectStreamHandler.ServeHTTP)
	}

	// Secret routes
	if surfaces.Recorded(cfg.Surfaces, surfaces.Secrets, cfg.SecretStore != nil,
		"sem banco — o SecretStore vem de `stores` (setup_server.go:230)") {
		secretHandler := secrets.NewSecretHandler(cfg.SecretStore)
		if cfg.SecretK8sSync != nil {
			secretHandler.SetK8sSync(cfg.SecretK8sSync)
		}
		// Sprint A T1.1 (closes comp-001 CRITICAL): wire the environment
		// lock so Set/DeleteSecret serialize with concurrent `loopzero deploy`
		// flows holding the same pg_advisory_xact_lock key.
		if cfg.SecretEnvLocker != nil {
			secretHandler.SetLocker(cfg.SecretEnvLocker)
		}
		// T1.2 (plan 2026-06-10-j22-openbao-transit-eso-sync v1.2): wire
		// the cross-tenant binding check (EC-03 absorption / CWE-204
		// defense). Production injects *store.PostgresProjectStore directly.
		if cfg.SecretProjectChecker != nil {
			secretHandler.SetProjectChecker(cfg.SecretProjectChecker)
		}
		// T2.3+T2.5 (plan 2026-06-10-j22-openbao-transit-eso-sync v1.2):
		// wire the OpenBao transit encrypt path into Bulk so plaintext
		// never reaches PG. Production injects *vault.TransitClient via
		// cmd/setup_transit.go; the secret store also dispatches through
		// the SAME vault client (NewPostgresSecretStoreWithVault) so
		// reads decrypt correctly. encryptForStore detects the
		// vault:v1: prefix and passes through (no double-encrypt).
		if cfg.SecretTransit != nil {
			secretHandler.SetTransitClient(cfg.SecretTransit)
		}
		r.Get("/api/v1/secrets", secretHandler.List)
		r.With(rateLimitMiddlewares...).Post("/api/v1/secrets", secretHandler.Set)
		// J-22 T1.1: bulk endpoint POST /api/v1/secrets/bulk.
		// Registered BEFORE /secrets/{name} so chi v5.2.5 radix tree resolves the
		// literal `/bulk` prefix before the `{name}` wildcard. The reverse order
		// causes chi to route POST /secrets/bulk through the {name} branch where
		// only DELETE is registered → 405 METHOD_NOT_ALLOWED (caught LIVE 2026-06-13
		// sponsor walkthrough post v0.14.0; loopzero-api log confirmed status=405).
		r.With(rateLimitMiddlewares...).Post("/api/v1/secrets/bulk", secretHandler.Bulk)
		r.With(rateLimitMiddlewares...).Delete("/api/v1/secrets/{name}", secretHandler.DeleteSecret)
	}

	// Build routes — use the single BuildHandler from Config (C-08).
	//
	// Toda esta familia cai junto: `NewBuildHandler` devolve nil quando `orchDeps`
	// e nil (main.go:477), e `orchDeps` so' e construido dentro de
	// `if dbAdapter != nil` (main.go:167). A cadeia inteira comeca no banco.
	if surfaces.Recorded(cfg.Surfaces, surfaces.Builds, cfg.BuildHandler != nil,
		"sem banco — a cadeia é banco → orchDeps → BuildHandler (main.go:167 e :477). "+
			"Desligada, nenhum deploy pode começar") {
		r.Get("/api/v1/builds/{id}", cfg.BuildHandler.Get)
		// Wave 1 T1.9: chain is rate-limit → idempotency → rate cap (buildEnforcement)
		// → cost cap (buildCostGate) → handler. Both enforcement layers can
		// short-circuit with HTTP 402 — rate cap surfaces "builds/month exceeded",
		// cost cap surfaces "build minutes exceeded" with different upgrade copy.
		r.With(append(idempotentMiddlewares, buildEnforcement, buildCostGate)...).Post("/api/v1/builds", cfg.BuildHandler.Create)
		r.With(rateLimitMiddlewares...).Post("/api/v1/builds/{id}/cancel", cfg.BuildHandler.Cancel)
	}

	// CHG-2026-04-25-ATTESTATION-WIRE Phase 6: supply-chain attestation
	// endpoint. Returns the cosign signature reference + sha256 digest
	// for a build's release. Behind requireAuth (parent group middleware).
	if surfaces.Recorded(cfg.Surfaces, surfaces.BuildAttestation, cfg.BuildAttestationHandler != nil,
		"sem banco — mesma cadeia da família de build (main.go:167). Desligada, a "+
			"cadeia de assinatura de uma release deixa de ser consultável") {
		r.Get("/api/v1/builds/{id}/attestation", cfg.BuildAttestationHandler.Get)
	}

	// Sprint 6 / T1.2: per-app SBOM endpoint. Returns metadata + presigned
	// download URL for the CycloneDX/SPDX SBOM of every signed image in
	// the build. Behind requireAuth (parent group middleware).
	if surfaces.Recorded(cfg.Surfaces, surfaces.BuildSBOM, cfg.BuildSBOMHandler != nil,
		"sem banco — mesma cadeia da família de build (main.go:167)") {
		r.Get("/api/v1/builds/{id}/sbom", cfg.BuildSBOMHandler.Get)
	}

	if surfaces.Recorded(cfg.Surfaces, surfaces.BuildLogs, cfg.LogsHandler != nil,
		"sem banco — vem de `orchDeps` (setup_server.go:236), que exige `dbAdapter`") {
		r.Get("/api/v1/builds/{id}/logs", cfg.LogsHandler.Get)
	}
	if surfaces.Recorded(cfg.Surfaces, surfaces.LogStream, cfg.LogStreamHandler != nil,
		"sem banco — vem de `orchDeps`. Desligada, o acompanhamento ao vivo de um "+
			"deploy some e o cliente fica sem retorno durante a construção") {
		r.Get("/api/v1/builds/{id}/logs/stream", cfg.LogStreamHandler.StreamBuildLogs)
		r.Get("/api/v1/projects/{name}/logs/stream", cfg.LogStreamHandler.StreamRuntimeLogs)
	}
	if surfaces.Recorded(cfg.Surfaces, surfaces.ProjectStatus, cfg.StatusHandler != nil,
		"sem banco — vem de `orchDeps`") {
		r.Get("/api/v1/projects/{name}/status", cfg.StatusHandler.Get)
	}
	// T1.2 J-18 — Prometheus query_range proxy for the SignalDetailSheet
	// histograms (ADR D2, plan 2026-06-06-dashboard-journey-j18-status-runtime-health v1.1).
	//
	// AUSENCIA e DEGRADACAO sao estados DIFERENTES, e este comentario dizia que
	// eram o mesmo. Ele afirmava que o handler devolve 503
	// `prometheus_not_configured` "quando MetricsProxyHandler e nil" — mas com o
	// handler nil a rota nem e registrada, entao a resposta e 404. Medido:
	// `NewMetricsProxyHandler` (metrics_proxy.go:76) NUNCA devolve nil; quem pode
	// ser nil e o cliente do Prometheus, e e' esse caso que produz o 503.
	// Um operador seguindo o texto anterior iria checar LOOPZERO_PROMETHEUS_ADDRESS
	// quando o problema e' o banco.
	if surfaces.Recorded(cfg.Surfaces, surfaces.MetricsProxy, cfg.MetricsProxyHandler != nil,
		"sem banco — vem de `orchDeps`. Isto NAO e' o mesmo que o Prometheus nao "+
			"estar configurado: naquele caso o handler existe e devolve 503") {
		r.Get("/api/v1/projects/{name}/metrics", cfg.MetricsProxyHandler.Get)
	}
	// T2.1 J-18 T1.3.b — SSE signal.changed stream for J-18 useStatusStream
	// consumer; backed by canonical sse.EventStore infrastructure.
	if surfaces.Recorded(cfg.Surfaces, surfaces.StatusStream, cfg.StatusSSEHandler != nil,
		"sem banco — vem de `orchDeps`") {
		r.Get("/api/v1/projects/{name}/signals/stream", cfg.StatusSSEHandler.Get)
	}
	// T5.1 — canonical Deploy Timeline SSE stream per plan
	// deploy-timeline-canonical-5-phases. The TenantResolver pulls
	// tenant from mwauth.GetUserContext (same auth contract as
	// every other authenticated route in this group).
	if cfg.DeployTimelineStore != nil {
		timeline.RegisterDeployTimelineRoute(r, cfg.DeployTimelineStore,
			func(ctx context.Context) (string, bool) {
				uc := mwauth.GetUserContext(ctx)
				if uc == nil || uc.TenantID == "" {
					return "", false
				}
				return uc.TenantID, true
			})
	}
	if surfaces.Recorded(cfg.Surfaces, surfaces.Scale, cfg.ScaleHandler != nil,
		"sem banco — vem de `orchDeps` (setup_server.go), que exige `dbAdapter`") {
		r.With(rateLimitMiddlewares...).Post("/api/v1/projects/{name}/scale", cfg.ScaleHandler.Scale)
	}
	if surfaces.Recorded(cfg.Surfaces, surfaces.BuildList, cfg.BuildListHandler != nil,
		"sem banco — vem de `orchDeps` (setup_server.go), que só é construído dentro de `if dbAdapter != nil` (main.go:167)") {
		r.Get("/api/v1/projects/{name}/builds", cfg.BuildListHandler.List)
		// T2.2 + ADR D3 + EC-2: distinct envs the project has built into,
		// feeds dashboard /deployments dynamic filter chips. The handler
		// enforces the cross-tenant gate via the ProjectStore wired in
		// NewBuildListHandlerWithGate (setup_orchestrator.go).
		r.Get("/api/v1/projects/{name}/build-envs", cfg.BuildListHandler.BuildEnvs)
	}
	if surfaces.Recorded(cfg.Surfaces, surfaces.BuildStats, cfg.BuildStatsHandler != nil,
		"sem banco — vem de `orchDeps`. Desligada, o painel perde o agregado de deploys") {
		// J-06 D2: tenant-scoped deploys aggregate for the Overview sparkline.
		// Static /builds/stats — chi matches it before /projects/{name}/builds.
		r.Get("/api/v1/builds/stats", cfg.BuildStatsHandler.Stats)
	}
	if surfaces.Recorded(cfg.Surfaces, surfaces.Rollback, cfg.RollbackHandler != nil,
		"sem banco — vem de `orchDeps`. Desligada, não há caminho de reversão de uma release") {
		r.With(rateLimitMiddlewares...).Post("/api/v1/projects/{name}/rollback", cfg.RollbackHandler.Create)
	}
	if surfaces.Recorded(cfg.Surfaces, surfaces.Promote, cfg.PromoteHandler != nil,
		"sem banco — vem de `orchDeps`") {
		r.With(rateLimitMiddlewares...).Post("/api/v1/projects/{name}/promote", cfg.PromoteHandler.Create)
	}

	// Team routes
	if surfaces.Recorded(cfg.Surfaces, surfaces.Teams, cfg.TeamHandler != nil,
		"sem banco — construído sobre `PostgresTeamStore` (setup_server.go:283)") {
		r.Get("/api/v1/teams/members", cfg.TeamHandler.List)
		r.With(append(rateLimitMiddlewares, seatEnforcement)...).Post("/api/v1/teams/members", cfg.TeamHandler.Add)
		r.With(rateLimitMiddlewares...).Delete("/api/v1/teams/members/{username}", cfg.TeamHandler.Remove)
		r.With(rateLimitMiddlewares...).Patch("/api/v1/teams/members/{username}", cfg.TeamHandler.UpdateRole)
	}

	// Tenant deletion routes (MEET-052)
	if surfaces.Recorded(cfg.Surfaces, surfaces.TenantDeletion, cfg.TenantDeletionStore != nil,
		"NUNCA ATRIBUIDO — medido em 2026-08-16: `Config.TenantDeletionStore` e declarado e nao recebe valor em nenhum ponto do codigo de producao, entao estas tres rotas nunca existiram em deployment nenhum. Nao e falta de dependencia: e falta de fiacao") {
		tenantHandler := tenants.NewTenantHandler(cfg.TenantDeletionStore, cfg.Logger)
		r.With(rateLimitMiddlewares...).Delete("/api/v1/tenants/{id}", tenantHandler.Delete)
		r.With(rateLimitMiddlewares...).Post("/api/v1/tenants/{id}/restore", tenantHandler.Restore)
		r.With(append(rateLimitMiddlewares, mwauth.RequireAdmin)...).Post("/api/v1/tenants/bulk-delete", tenantHandler.Delete)
	}

	// Billing routes — T1.6 of deep-review-remediation-plan (closes
	// find_billing_route_not_wired HIGH). CLI commands loopzero billing and
	// loopzero billing portal hit silent 404 pre-T1.6.
	if surfaces.Recorded(cfg.Surfaces, surfaces.Billing, cfg.BillingHandler != nil,
		"sem banco — construído dentro do bloco que exige `dbAdapter` (setup_server.go:255). As chaves do Stripe NAO desligam esta rota: sem elas o checkout responde 503 tipado, que e outro estado") {
		r.Get("/api/v1/billing", cfg.BillingHandler.Get)
		// `owner` porque o loopzero-cloud exige o mesmo para a MESMA capacidade
		// (loopzero-cloud/cmd/main.go:1572). A rota estava com rate-limit e mais nada, e a
		// divergencia sobreviveu porque nenhuma leitura de UM lado so a mostraria: e
		// escalonamento horizontal — qualquer membro do time abria o portal de cobranca
		// dele, com fatura, meio de pagamento e historico. As duas hierarquias sao
		// identicas (`owner: 3`), entao alinhar foi aplicar o middleware que ja existia.
		r.With(append(rateLimitMiddlewares, mwauth.RequireRole("owner"))...).Post("/api/v1/billing/portal", cfg.BillingHandler.Portal)
	}

	// Environment routes
	if surfaces.Recorded(cfg.Surfaces, surfaces.Environments, cfg.EnvironmentHandler != nil,
		"sem o cliente de Kubernetes ou sem banco — construído no literal do Config (setup_server.go:131) sobre `k8sDeps.TenantCRMgr` e `dbAdapter.Pool`, entao em producao ele e sempre nao-nil e esta guarda serve aos testes") {
		r.Get("/api/v1/environments", cfg.EnvironmentHandler.List)
		r.Get("/api/v1/environments/{name}", cfg.EnvironmentHandler.Get)
		r.With(append(rateLimitMiddlewares, envEnforcement)...).Post("/api/v1/environments", cfg.EnvironmentHandler.Create)
		r.With(rateLimitMiddlewares...).Delete("/api/v1/environments/{name}", cfg.EnvironmentHandler.Delete)
	}

	// Custom domain routes (MEET-2026-04-05-033)
	if surfaces.Recorded(cfg.Surfaces, surfaces.Domains, cfg.DomainHandler != nil,
		"passado por quem monta o servidor (parâmetro de setupServerConfig, setup_server.go:75) — nil quando o chamador não o construiu") {
		r.Get("/api/v1/domains", cfg.DomainHandler.List)
		r.Get("/api/v1/domains/{domain}", cfg.DomainHandler.Get)
		r.With(append(rateLimitMiddlewares, idempotentMiddlewares...)...).Post("/api/v1/domains", cfg.DomainHandler.Create)
		// J11D7 LOCKED ADR: /verify uses per-(tenant, domain) limiter (1 req/30s)
		// instead of the tenant-wide 60/min limiter. Falls back to the tenant
		// limiter when VerifyRateLimiter is not wired (fail-open per ADR D3).
		verifyMiddlewares := rateLimitMiddlewares
		if cfg.VerifyRateLimiter != nil {
			verifyMiddlewares = append([]func(http.Handler) http.Handler{cfg.VerifyRateLimiter.Middleware()}, rateLimitMiddlewares...)
		}
		r.With(verifyMiddlewares...).Post("/api/v1/domains/{domain}/verify", cfg.DomainHandler.Verify)
		r.With(rateLimitMiddlewares...).Delete("/api/v1/domains/{domain}", cfg.DomainHandler.Delete)
	}

	// Repository connection routes (GitHub App — auto-deploy)
	if surfaces.Recorded(cfg.Surfaces, surfaces.Repositories, cfg.RepositoryHandler != nil,
		"sem as dependências do GitHub — vem de `ghDeps` (setup_server.go:252), que exige credenciais do GitHub App e, antes delas, o banco") {
		r.Post("/api/v1/projects/{name}/connect", cfg.RepositoryHandler.Connect)
		r.Delete("/api/v1/projects/{name}/connect", cfg.RepositoryHandler.Disconnect)
		r.Get("/api/v1/projects/{name}/connect", cfg.RepositoryHandler.GetConnection)
	}

	// Plan J-09 T2.1 (2026-06-05): dashboard repo picker source.
	// Lists every repo the given installation has authorised, sanitised to
	// {full_name, default_branch, private, language}. Tenant-scoped via the
	// installation row; cross-tenant returns 403.
	if surfaces.Recorded(cfg.Surfaces, surfaces.IntegrationsGitHub, cfg.IntegrationsGitHubHandler != nil,
		"sem as dependências do GitHub — mesma cadeia de `ghDeps`") {
		r.Get("/api/v1/integrations/github/installations/{id}/repositories", cfg.IntegrationsGitHubHandler.ListRepositories)
	}

	// Preview environment routes (Sprint 5 / T2.3).
	// URL shape matches the CLI: /api/v1/projects/{project}/previews[...].
	// POST is rate-limited and idempotent (creating the same PR twice
	// upserts into the same row). DELETE is rate-limited.
	if surfaces.Recorded(cfg.Surfaces, surfaces.Previews, cfg.PreviewHandler != nil,
		"sem banco — construído sobre `PostgresProjectStore` (setup_server.go:369)") {
		r.Get("/api/v1/projects/{project}/previews", cfg.PreviewHandler.List)
		r.Get("/api/v1/projects/{project}/previews/{id}", cfg.PreviewHandler.Get)
		r.With(rateLimitMiddlewares...).Delete("/api/v1/projects/{project}/previews/{id}", cfg.PreviewHandler.Delete)
		r.With(append(idempotentMiddlewares, rateLimitMiddlewares...)...).Post("/api/v1/projects/{project}/previews", cfg.PreviewHandler.Create)
	}

	if surfaces.Recorded(cfg.Surfaces, surfaces.Audit, cfg.AuditHandler != nil,
		"sem banco — construído sobre `PostgresAuditStore` (setup_server.go:289). Desligada, o registro de auditoria deixa de ser consultável") {
		r.Get("/api/v1/audit", cfg.AuditHandler.List)
	}
	// NOTE: Admin endpoints are deliberately registered OUTSIDE this function
	// (in registerRoutes, alongside the GitHub webhook). They use only
	// X-Service-Auth — putting them under mwauth.Auth (which requires
	// X-LoopZero-User-ID/Tenant identity headers too) breaks operator CLI auth.
	// See Achado #25 (CHG-2026-04-29-DOGFOOD iter 13).
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
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
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
