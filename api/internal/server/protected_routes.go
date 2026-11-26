// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	mwaudit "github.com/getloopscale/loop-runtime/api/internal/middleware/audit"
	mwauth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	policy "github.com/getloopscale/loop-runtime/api/internal/middleware/policy"
	"github.com/getloopscale/loop-runtime/api/internal/projects/projectshttp"
	"github.com/getloopscale/loop-runtime/api/internal/secrets/secretshttp"
	"github.com/getloopscale/loop-runtime/api/internal/source/sourcehttp"
	"github.com/getloopscale/loop-runtime/api/internal/sse"
	"github.com/getloopscale/loop-runtime/api/internal/surfaces"
	"github.com/getloopscale/loop-runtime/api/internal/tenants/tenantshttp"
	"github.com/getloopscale/loop-runtime/api/internal/timeline/timelinehttp"
	"github.com/getloopscale/loop-runtime/pkg/enforcement"
)

// middleware is one link of an HTTP middleware chain.
type middleware = func(http.Handler) http.Handler

// routeChains is the middleware vocabulary every protected route composes
// from. Built once per router so a route names the chain it needs instead of
// re-assembling slices by hand — hand assembly is how the rate limiter once
// ran twice on the same request.
type routeChains struct {
	// rateLimited guards every mutation (mutation_guards_test.go enforces it).
	rateLimited []middleware
	// idempotent is rateLimited plus Idempotency-Key handling. The key is
	// REQUIRED on these routes (400 MISSING_IDEMPOTENCY_KEY without it); which
	// mutations use it, and why the others do not, is recorded route by route
	// in mutation_guards_test.go.
	idempotent []middleware

	buildQuota       middleware // plan: builds_per_month
	buildMinutes     middleware // cost cap: build_minutes
	environmentQuota middleware // plan: permanent environments
	seatQuota        middleware // plan: seats
}

// limited returns the rate-limited chain followed by extra.
func (c routeChains) limited(extra ...middleware) []middleware {
	return concatMiddleware(c.rateLimited, extra)
}

// idempotentWith returns the idempotent chain followed by extra.
func (c routeChains) idempotentWith(extra ...middleware) []middleware {
	return concatMiddleware(c.idempotent, extra)
}

// gatedBy puts the role gate (auth.RequireRole) in front of a chain: a caller
// below minRole is refused before it spends a rate-limit token or leaves an
// idempotency record, and every role-gated route answers with the same
// INSUFFICIENT_ROLE shape.
func gatedBy(minRole string, chain []middleware) []middleware {
	return concatMiddleware([]middleware{mwauth.RequireRole(minRole)}, chain)
}

// audited declares a route's audit identity (see audit.Declare): the action
// name and resource type its audit row records.
func audited(action, resourceType string) middleware {
	return mwaudit.Declare(action, resourceType)
}

// concatMiddleware always allocates, so no two routes share a backing array.
func concatMiddleware(parts ...[]middleware) []middleware {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]middleware, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// newRouteChains assembles the chains from the configuration. The plan and
// cost gates are pass-through when their limiter is unset.
func newRouteChains(cfg Config) routeChains {
	var c routeChains
	if cfg.Guards.RateLimiter != nil {
		c.rateLimited = []middleware{cfg.Guards.RateLimiter.Middleware()}
	}
	c.idempotent = c.limited()
	if cfg.Guards.IdempotencyStore != nil {
		c.idempotent = c.idempotentWith(policy.Idempotency(cfg.Guards.IdempotencyStore))
	}

	upgradeURL := cfg.Guards.UpgradeURL
	if upgradeURL == "" {
		upgradeURL = "/billing"
	}
	usage := cfg.Guards.planUsage()
	planGate := func(metric string, count enforcement.UsageCounter) middleware {
		return policy.PlanLimits(cfg.Guards.PlanLimiter, policy.PlanLimitCheck{Metric: metric, Count: count},
			tenantFromRequest, upgradeURL, cfg.Logger)
	}
	// M4 (account-level tenancy, Phase 2): there is no per-tenant projects
	// gate here — the project cap is enforced per Account at the single
	// creation path (PostgresProjectStore, advisory-lock count+insert).
	c.buildQuota = planGate("builds_per_month", usage.BuildsThisMonth)
	// D5 (preview single-slot plan): only PERMANENT environments count
	// against plan_limits.environments; previews live in preview_slot.
	c.environmentQuota = planGate("environments", usage.PermanentEnvironments)
	c.seatQuota = planGate("seats", usage.Seats)
	// T1.9 (Wave 1): the cost cap is distinct from the builds/month rate cap.
	c.buildMinutes = cfg.Guards.CostEnforcer.Middleware(enforcementMetricBuildMinutes)
	return c
}

func tenantFromRequest(r *http.Request) string {
	return tenantFromContext(r.Context())
}

func tenantFromContext(ctx context.Context) string {
	if uc := mwauth.GetUserContext(ctx); uc != nil {
		return uc.TenantID
	}
	return ""
}

// protectedRouteFamily mounts one family of authenticated routes. Each
// family decides for itself whether its dependencies are present and
// records the decision as a surface.
type protectedRouteFamily func(r chi.Router, cfg Config, ch routeChains)

// protectedRouteFamilies lists every family, in registration order. Adding a
// route family is adding an entry here — no existing family changes.
var protectedRouteFamilies = []protectedRouteFamily{
	registerSourceRoutes,
	registerProjectRoutes,
	registerEventStreamRoutes,
	registerSecretRoutes,
	registerBuildRoutes,
	registerBuildReadRoutes,
	registerProjectReadRoutes,
	registerReleaseRoutes,
	registerTeamRoutes,
	registerTenantRoutes,
	registerBillingRoutes,
	registerEnvironmentRoutes,
	registerDomainRoutes,
	registerRepositoryRoutes,
	registerPreviewRoutes,
	registerAuditRoutes,
}

// registerProtectedRoutes wires all authenticated API routes.
//
// Admin endpoints are deliberately registered OUTSIDE this group (alongside
// the GitHub webhook): they authenticate with X-Service-Auth only, and the
// user-auth middleware here would demand identity headers operator calls do
// not carry (Achado #25, CHG-2026-04-29-DOGFOOD iter 13).
func registerProtectedRoutes(r chi.Router, cfg Config, ch routeChains) {
	for _, register := range protectedRouteFamilies {
		register(r, cfg, ch)
	}
}

func registerSourceRoutes(r chi.Router, cfg Config, ch routeChains) {
	if !surfaces.Recorded(cfg.Surfaces, surfaces.SourceUpload, cfg.SourceStore != nil,
		"no database — the SourceStore comes from `stores` (setup_server.go), which requires `dbAdapter`. "+
			"Without it the first step of every deploy — uploading the bundle — disappears") {
		return
	}
	var opts []func(*sourcehttp.SourceHandler)
	if cfg.ObjectUploader != nil {
		opts = append(opts, sourcehttp.WithObjectUploader(cfg.ObjectUploader))
	}
	if cfg.SourceBucket != "" {
		opts = append(opts, sourcehttp.WithSourceBucket(cfg.SourceBucket))
	}
	opts = append(opts, sourcehttp.WithEnvBlocklistMode(cfg.EnvBlocklistMode))
	h := sourcehttp.NewSourceHandler(cfg.SourceStore, opts...)
	// Rate limited, not idempotent: a 500 MB body is not hashed for replay.
	r.With(ch.limited(audited("source.uploaded", "source"))...).Post("/api/v1/source/upload", h.Upload)
}

func registerProjectRoutes(r chi.Router, cfg Config, ch routeChains) {
	if !surfaces.Recorded(cfg.Surfaces, surfaces.Projects,
		cfg.ProjectStore != nil && cfg.TenantCRMgr != nil,
		"no database or no cluster access — the ProjectStore comes from `dbDeps` and the "+
			"TenantCRMgr from the Kubernetes client; the routes need both") {
		return
	}
	h := projectshttp.NewProjectHandler(cfg.ProjectStore, cfg.TenantCRMgr)
	if cfg.ProjectDeleter != nil {
		h.SetDeleter(cfg.ProjectDeleter)
	}
	// QA Run-3 #123: the canonical tier resolver, so Create enforces plan caps.
	if cfg.PlanTierStore != nil {
		h.SetPlanTierStore(cfg.PlanTierStore)
	}
	if cfg.TenantCellResolver != nil {
		h.SetTenantCellResolver(cfg.TenantCellResolver)
	}
	// T4.1 (m2-operator-canonical-fix): POST /projects auto-creates `production`.
	if cfg.ProjectEnvEnsurer != nil {
		h.SetEnvEnsurer(cfg.ProjectEnvEnsurer)
	}
	r.Get("/api/v1/projects", h.List)
	r.Get("/api/v1/projects/{name}", h.Get)
	r.With(ch.limited(audited("project.created", "project"))...).Post("/api/v1/projects", h.Create)
	r.With(ch.limited(audited("project.deleted", "project"))...).Delete("/api/v1/projects/{name}", h.Delete)
	r.With(ch.limited(audited("environment.deleted", "environment"))...).Delete("/api/v1/projects/{name}/environments/{env}", h.DeleteEnvironment)
}

func registerEventStreamRoutes(r chi.Router, cfg Config, _ routeChains) {
	if !surfaces.Recorded(cfg.Surfaces, surfaces.EventsStream,
		cfg.EventStore != nil && cfg.BuildStoreReader != nil,
		"no database — both come from `stores`. Without them the CLI cannot follow a deploy "+
			"and cannot tell whether it progressed") {
		return
	}
	buildOpts := []sse.StreamOption{sse.WithLogger(cfg.Logger)}
	// Plan J-17 T2.3 — the project stream reuses the build stream's knobs so
	// operators configure one set of intervals.
	projectOpts := []sse.ProjectStreamOption{sse.WithProjectLogger(cfg.Logger)}
	if cfg.SSE.HeartbeatInterval > 0 {
		buildOpts = append(buildOpts, sse.WithHeartbeatInterval(cfg.SSE.HeartbeatInterval))
		projectOpts = append(projectOpts, sse.WithProjectHeartbeatInterval(cfg.SSE.HeartbeatInterval))
	}
	if cfg.SSE.PollInterval > 0 {
		buildOpts = append(buildOpts, sse.WithPollInterval(cfg.SSE.PollInterval))
		projectOpts = append(projectOpts, sse.WithProjectPollInterval(cfg.SSE.PollInterval))
	}
	if cfg.SSE.SafetyTimeout > 0 {
		buildOpts = append(buildOpts, sse.WithSafetyTimeout(cfg.SSE.SafetyTimeout))
	}
	if cfg.SSE.MaxConcurrentStreams > 0 {
		buildOpts = append(buildOpts, sse.WithMaxConcurrentStreams(cfg.SSE.MaxConcurrentStreams))
		projectOpts = append(projectOpts, sse.WithProjectMaxConcurrentStreams(cfg.SSE.MaxConcurrentStreams))
	}
	r.Get("/api/v1/builds/{id}/stream", sse.NewStreamHandler(cfg.EventStore, cfg.BuildStoreReader, buildOpts...).ServeHTTP)
	r.Get("/api/v1/projects/{name}/builds/stream", sse.NewProjectStreamHandler(cfg.EventStore, projectOpts...).ServeHTTP)
}

// registerSecretRoutes: Sprint A T1.1 (comp-001) — every secret mutation runs
// under the environment lock, so the routes exist only when the lock does.
func registerSecretRoutes(r chi.Router, cfg Config, ch routeChains) {
	if !surfaces.Recorded(cfg.Surfaces, surfaces.Secrets, cfg.Secrets.Store != nil && cfg.Secrets.EnvLocker != nil,
		"no database — the SecretStore and the environment locker both come from `stores` "+
			"(setup_server.go), and a secret mutation outside the lock races a deploy") {
		return
	}
	h := secretshttp.NewSecretHandler(cfg.Secrets.Store, cfg.Secrets.EnvLocker)
	if cfg.Secrets.K8sSync != nil {
		h.SetK8sSync(cfg.Secrets.K8sSync)
	}
	// SECRET_* domain audit rows and the J-22 T1.3 per-environment plan gate.
	if cfg.Secrets.AuditLogger != nil {
		h.SetAuditLogger(cfg.Secrets.AuditLogger)
	}
	if cfg.Guards.PlanLimiter != nil {
		h.SetPlanLimiter(cfg.Guards.PlanLimiter)
		h.SetUpgradeURL(cfg.Guards.UpgradeURL)
	}
	// T1.2 (j22 transit plan): cross-tenant binding check (EC-03 / CWE-204).
	if cfg.Secrets.ProjectChecker != nil {
		h.SetProjectChecker(cfg.Secrets.ProjectChecker)
	}
	r.Get("/api/v1/secrets", h.List)
	r.With(ch.limited(audited("secret.set", "secret"))...).Post("/api/v1/secrets", h.Set)
	// J-22 T1.1: /secrets/bulk is registered BEFORE /secrets/{name} so chi
	// resolves the literal before the wildcard (the reverse answered 405).
	r.With(ch.limited(audited("secret.set", "secret"))...).Post("/api/v1/secrets/bulk", h.Bulk)
	r.With(ch.limited(audited("secret.deleted", "secret"))...).Delete("/api/v1/secrets/{name}", h.DeleteSecret)
}

func registerBuildRoutes(r chi.Router, cfg Config, ch routeChains) {
	if !surfaces.Recorded(cfg.Surfaces, surfaces.Builds, cfg.BuildHandler != nil,
		"no database — the chain is database → orchDeps → BuildHandler (main.go). "+
			"Without it no deploy can start") {
		return
	}
	r.Get("/api/v1/builds/{id}", cfg.BuildHandler.Get)
	// Wave 1 T1.9: rate limit → idempotency → builds/month cap → build-minutes
	// cap → handler. Either cap can answer 402 with its own upgrade copy.
	r.With(ch.idempotentWith(audited("build.submitted", "build"), ch.buildQuota, ch.buildMinutes)...).Post("/api/v1/builds", cfg.BuildHandler.Create)
	r.With(ch.limited(audited("build.cancelled", "build"))...).Post("/api/v1/builds/{id}/cancel", cfg.BuildHandler.Cancel)
}

// registerBuildReadRoutes mounts the read-only views over one build.
func registerBuildReadRoutes(r chi.Router, cfg Config, _ routeChains) {
	// CHG-2026-04-25-ATTESTATION-WIRE Phase 6: cosign signature + digest.
	if surfaces.Recorded(cfg.Surfaces, surfaces.BuildAttestation, cfg.BuildAttestationHandler != nil,
		"no database — same chain as the build family (main.go). Without it a release's "+
			"signing chain can no longer be queried") {
		r.Get("/api/v1/builds/{id}/attestation", cfg.BuildAttestationHandler.Get)
	}
	// Sprint 6 / T1.2: per-app SBOM metadata + presigned download URLs.
	if surfaces.Recorded(cfg.Surfaces, surfaces.BuildSBOM, cfg.BuildSBOMHandler != nil,
		"no database — same chain as the build family (main.go)") {
		r.Get("/api/v1/builds/{id}/sbom", cfg.BuildSBOMHandler.Get)
		r.Get("/api/v1/builds/{id}/sbom/{app}/download", cfg.BuildSBOMHandler.Download)
	}
	if surfaces.Recorded(cfg.Surfaces, surfaces.BuildLogs, cfg.LogsHandler != nil,
		"no database — comes from `orchDeps` (setup_server.go), which requires `dbAdapter`") {
		// ?mode=debug is for operators: the admin flag must be on and the
		// caller must carry the admin service secret — the same question the
		// admin routes ask.
		logsHandler := cfg.LogsHandler.WithDebugAuthorizer(func(r *http.Request) bool {
			return cfg.Admin.APIEnabled && cfg.Auth.AdminServiceAuthorized(r)
		})
		r.Get("/api/v1/builds/{id}/logs", logsHandler.Get)
	}
	if surfaces.Recorded(cfg.Surfaces, surfaces.LogStream, cfg.LogStreamHandler != nil,
		"no database — comes from `orchDeps`. Without it live deploy output disappears and "+
			"the client gets no feedback while the build runs") {
		r.Get("/api/v1/builds/{id}/logs/stream", cfg.LogStreamHandler.StreamBuildLogs)
		r.Get("/api/v1/projects/{name}/logs/stream", cfg.LogStreamHandler.StreamRuntimeLogs)
	}
	if surfaces.Recorded(cfg.Surfaces, surfaces.BuildStats, cfg.BuildStatsHandler != nil,
		"no database — comes from `orchDeps`. Without it the dashboard loses the deploys aggregate") {
		// J-06 D2: the static /builds/stats is matched before /projects/{name}/builds.
		r.Get("/api/v1/builds/stats", cfg.BuildStatsHandler.Stats)
	}
}

// registerProjectReadRoutes mounts the read-only runtime views of a project.
func registerProjectReadRoutes(r chi.Router, cfg Config, _ routeChains) {
	if surfaces.Recorded(cfg.Surfaces, surfaces.ProjectStatus, cfg.StatusHandler != nil,
		"no database — comes from `orchDeps`") {
		r.Get("/api/v1/projects/{name}/status", cfg.StatusHandler.Get)
	}
	// T1.2 J-18 — Prometheus query_range proxy. An ABSENT handler (404, no
	// database) and an unconfigured Prometheus (the handler exists and answers
	// 503 prometheus_not_configured) are different states.
	if surfaces.Recorded(cfg.Surfaces, surfaces.MetricsProxy, cfg.MetricsProxyHandler != nil,
		"no database — comes from `orchDeps`. This is NOT Prometheus being unconfigured: "+
			"in that case the handler exists and answers 503") {
		r.Get("/api/v1/projects/{name}/metrics", cfg.MetricsProxyHandler.Get)
	}
	// T2.1 J-18 T1.3.b — SSE signal.changed stream.
	if surfaces.Recorded(cfg.Surfaces, surfaces.StatusStream, cfg.StatusSSEHandler != nil,
		"no database — comes from `orchDeps`") {
		r.Get("/api/v1/projects/{name}/signals/stream", cfg.StatusSSEHandler.Get)
	}
	if surfaces.Recorded(cfg.Surfaces, surfaces.BuildList, cfg.BuildListHandler != nil,
		"no database — comes from `orchDeps` (setup_server.go), built only inside `if dbAdapter != nil` (main.go)") {
		r.Get("/api/v1/projects/{name}/builds", cfg.BuildListHandler.List)
		// T2.2 + ADR D3 + EC-2: distinct envs the project has built into.
		r.Get("/api/v1/projects/{name}/build-envs", cfg.BuildListHandler.BuildEnvs)
	}
	// T5.1 — canonical Deploy Timeline, tenant resolved from the auth context.
	if cfg.DeployTimelineStore != nil {
		timelinehttp.RegisterDeployTimelineRoute(r, cfg.DeployTimelineStore, func(ctx context.Context) (string, bool) {
			tenantID := tenantFromContext(ctx)
			return tenantID, tenantID != ""
		})
	}
}

// registerReleaseRoutes mounts the mutations of a running project's release.
func registerReleaseRoutes(r chi.Router, cfg Config, ch routeChains) {
	if surfaces.Recorded(cfg.Surfaces, surfaces.Scale, cfg.ScaleHandler != nil,
		"no database — comes from `orchDeps` (setup_server.go), which requires `dbAdapter`") {
		r.With(ch.limited(audited("project.scaled", "project"))...).Post("/api/v1/projects/{name}/scale", cfg.ScaleHandler.Scale)
	}
	if surfaces.Recorded(cfg.Surfaces, surfaces.Rollback, cfg.RollbackHandler != nil,
		"no database — comes from `orchDeps`. Without it there is no way to revert a release") {
		r.With(ch.limited(audited("rollback.executed", "release"))...).Post("/api/v1/projects/{name}/rollback", cfg.RollbackHandler.Create)
		// Each rollback as an operation of its own: who, from and to which
		// release, and how it ended.
		r.Get("/api/v1/projects/{name}/rollbacks", cfg.RollbackHandler.List)
	}
	if surfaces.Recorded(cfg.Surfaces, surfaces.Promote, cfg.PromoteHandler != nil,
		"no database — comes from `orchDeps`") {
		r.With(ch.limited(audited("release.promoted", "release"))...).Post("/api/v1/projects/{name}/promote", cfg.PromoteHandler.Create)
	}
}

func registerTeamRoutes(r chi.Router, cfg Config, ch routeChains) {
	if !surfaces.Recorded(cfg.Surfaces, surfaces.Teams, cfg.TeamHandler != nil,
		"no database — built on `PostgresTeamStore` (setup_server.go)") {
		return
	}
	r.Get("/api/v1/teams/members", cfg.TeamHandler.List)
	r.With(gatedBy("admin", ch.limited(audited("member.added", "member"), ch.seatQuota))...).Post("/api/v1/teams/members", cfg.TeamHandler.Add)
	r.With(gatedBy("admin", ch.limited(audited("member.removed", "member")))...).Delete("/api/v1/teams/members/{username}", cfg.TeamHandler.Remove)
	r.With(gatedBy("owner", ch.limited(audited("member.role_changed", "member")))...).Patch("/api/v1/teams/members/{username}", cfg.TeamHandler.UpdateRole)
}

// registerTenantRoutes: the soft-deletion cycle (MEET-052).
func registerTenantRoutes(r chi.Router, cfg Config, ch routeChains) {
	if !surfaces.Recorded(cfg.Surfaces, surfaces.TenantDeletion, cfg.TenantDeletionStore != nil,
		"no database — the TenantDeletionStore is built inside the `dbAdapter` block (setup_server.go)") {
		return
	}
	h := tenantshttp.NewTenantHandler(cfg.TenantDeletionStore, cfg.Logger, cfg.Auth.ValidateAdminAPIKey)
	r.With(ch.limited(audited("tenant.deleted", "tenant"))...).Delete("/api/v1/tenants/{id}", h.Delete)
	r.With(ch.limited(audited("tenant.restored", "tenant"))...).Post("/api/v1/tenants/{id}/restore", h.Restore)
}

// registerBillingRoutes: T1.6 (find_billing_route_not_wired).
func registerBillingRoutes(r chi.Router, cfg Config, ch routeChains) {
	if !surfaces.Recorded(cfg.Surfaces, surfaces.Billing, cfg.BillingHandler != nil,
		"no database — built inside the block that requires `dbAdapter` (setup_server.go). Missing "+
			"Stripe keys do NOT disable this route: checkout then answers a typed 503, a different state") {
		return
	}
	r.Get("/api/v1/billing", cfg.BillingHandler.Get)
	// `owner`, as loopruntime-cloud requires for the same capability: any member
	// opening the billing portal is horizontal escalation to invoices,
	// payment method and history.
	r.With(gatedBy("owner", ch.limited(audited("billing.portal_opened", "billing")))...).Post("/api/v1/billing/portal", cfg.BillingHandler.Portal)
}

func registerEnvironmentRoutes(r chi.Router, cfg Config, ch routeChains) {
	if !surfaces.Recorded(cfg.Surfaces, surfaces.Environments, cfg.EnvironmentHandler != nil,
		"no Kubernetes client or no database — built in the Config literal (setup_server.go) on "+
			"`k8sDeps.TenantCRMgr` and `dbAdapter.Pool`; in production it is always set, so this guard serves tests") {
		return
	}
	r.Get("/api/v1/environments", cfg.EnvironmentHandler.List)
	r.Get("/api/v1/environments/{name}", cfg.EnvironmentHandler.Get)
	r.With(ch.limited(audited("environment.created", "environment"), ch.environmentQuota)...).Post("/api/v1/environments", cfg.EnvironmentHandler.Create)
	r.With(ch.limited(audited("environment.deleted", "environment"))...).Delete("/api/v1/environments/{name}", cfg.EnvironmentHandler.Delete)
}

// registerDomainRoutes: custom domains (MEET-2026-04-05-033).
func registerDomainRoutes(r chi.Router, cfg Config, ch routeChains) {
	if !surfaces.Recorded(cfg.Surfaces, surfaces.Domains, cfg.DomainHandler != nil,
		"passed in by whoever assembles the server (setupServerConfig, setup_server.go) — nil when the caller did not build it") {
		return
	}
	// The plan caps custom domains per environment (custom_domains_per_env).
	if cfg.Guards.PlanLimiter != nil {
		cfg.DomainHandler.SetPlanLimiter(cfg.Guards.PlanLimiter)
		cfg.DomainHandler.SetUpgradeURL(cfg.Guards.UpgradeURL)
	}
	r.Get("/api/v1/domains", cfg.DomainHandler.List)
	r.Get("/api/v1/domains/{domain}", cfg.DomainHandler.Get)
	r.Get("/api/v1/domains/{domain}/dns-records", cfg.DomainHandler.ListDNSRecords)
	r.With(gatedBy("admin", ch.idempotentWith(audited("domain.added", "domain")))...).Post("/api/v1/domains", cfg.DomainHandler.Create)
	// J11D7 LOCKED ADR: /verify uses the per-(tenant, domain) limiter in front
	// of the tenant-wide one; without it, the tenant limiter alone (ADR D3).
	verify := ch.limited(audited("domain.verified", "domain"))
	if cfg.Guards.VerifyRateLimiter != nil {
		verify = concatMiddleware([]middleware{cfg.Guards.VerifyRateLimiter.Middleware()}, verify)
	}
	r.With(verify...).Post("/api/v1/domains/{domain}/verify", cfg.DomainHandler.Verify)
	r.With(gatedBy("admin", ch.limited(audited("domain.removed", "domain")))...).Delete("/api/v1/domains/{domain}", cfg.DomainHandler.Delete)
}

// registerRepositoryRoutes: GitHub App connection (auto-deploy), the
// installations a tenant has and the dashboard's repository picker (J-09 T2.1).
func registerRepositoryRoutes(r chi.Router, cfg Config, ch routeChains) {
	if surfaces.Recorded(cfg.Surfaces, surfaces.Repositories, cfg.RepositoryHandler != nil,
		"no GitHub dependencies — comes from `ghDeps` (setup_server.go), which needs GitHub App credentials and, before them, the database") {
		r.With(gatedBy("admin", ch.limited(audited("repository.connected", "repository")))...).Post("/api/v1/projects/{name}/connect", cfg.RepositoryHandler.Connect)
		r.With(gatedBy("admin", ch.limited(audited("repository.disconnected", "repository")))...).Delete("/api/v1/projects/{name}/connect", cfg.RepositoryHandler.Disconnect)
		r.Get("/api/v1/projects/{name}/connect", cfg.RepositoryHandler.GetConnection)
	}
	if surfaces.Recorded(cfg.Surfaces, surfaces.IntegrationsGitHub, cfg.IntegrationsGitHubHandler != nil,
		"no GitHub dependencies — same `ghDeps` chain") {
		r.Get("/api/v1/integrations/github/installations/{id}/repositories", cfg.IntegrationsGitHubHandler.ListRepositories)
		r.Get("/api/v1/integrations/github/installations", cfg.IntegrationsGitHubHandler.ListInstallations)
		// Binds installations to the caller's tenant after GitHub confirms the
		// user's access: an admin action with an audit row, like connect.
		r.With(gatedBy("admin", ch.limited(audited("github_installation.claimed", "github_installation")))...).Post("/api/v1/integrations/github/installations/claim", cfg.IntegrationsGitHubHandler.ClaimInstallations)
	}
}

// registerPreviewRoutes: preview environments (Sprint 5 / T2.3). POST is
// idempotent — creating the same PR twice upserts the same row.
func registerPreviewRoutes(r chi.Router, cfg Config, ch routeChains) {
	if !surfaces.Recorded(cfg.Surfaces, surfaces.Previews, cfg.PreviewHandler != nil,
		"no database — built on `PostgresProjectStore` (setup_server.go)") {
		return
	}
	r.Get("/api/v1/projects/{project}/previews", cfg.PreviewHandler.List)
	r.Get("/api/v1/projects/{project}/previews/{id}", cfg.PreviewHandler.Get)
	r.With(ch.limited(audited("preview.deleted", "preview"))...).Delete("/api/v1/projects/{project}/previews/{id}", cfg.PreviewHandler.Delete)
	r.With(ch.idempotentWith(audited("preview.created", "preview"))...).Post("/api/v1/projects/{project}/previews", cfg.PreviewHandler.Create)
}

func registerAuditRoutes(r chi.Router, cfg Config, _ routeChains) {
	if surfaces.Recorded(cfg.Surfaces, surfaces.Audit, cfg.AuditHandler != nil,
		"no database — built on `PostgresAuditStore` (setup_server.go). Without it the audit log can no longer be queried") {
		r.With(mwauth.RequireRole("admin")).Get("/api/v1/audit", cfg.AuditHandler.List)
	}
}
