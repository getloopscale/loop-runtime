// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loop-runtime/api/internal/audit/audithttp"
	"github.com/getloopscale/loop-runtime/api/internal/billing/billinghttp"
	"github.com/getloopscale/loop-runtime/api/internal/builds/buildshttp"
	"github.com/getloopscale/loop-runtime/api/internal/domains/domainshttp"
	"github.com/getloopscale/loop-runtime/api/internal/environments/environmentshttp"
	"github.com/getloopscale/loop-runtime/api/internal/integrations/integrationshttp"
	"github.com/getloopscale/loop-runtime/api/internal/logs/logshttp"
	policy "github.com/getloopscale/loop-runtime/api/internal/middleware/policy"
	ratelimit "github.com/getloopscale/loop-runtime/api/internal/middleware/ratelimit"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/api/internal/preview/previewhttp"
	"github.com/getloopscale/loop-runtime/api/internal/projectmetrics/projectmetricshttp"
	"github.com/getloopscale/loop-runtime/api/internal/release/promotehttp"
	"github.com/getloopscale/loop-runtime/api/internal/release/rollbackhttp"
	"github.com/getloopscale/loop-runtime/api/internal/repositories/repositorieshttp"
	"github.com/getloopscale/loop-runtime/api/internal/scaling/scalinghttp"
	"github.com/getloopscale/loop-runtime/api/internal/secrets/secretshttp"
	"github.com/getloopscale/loop-runtime/api/internal/source"
	"github.com/getloopscale/loop-runtime/api/internal/sse"
	"github.com/getloopscale/loop-runtime/api/internal/status/statushttp"
	"github.com/getloopscale/loop-runtime/api/internal/surfaces"
	"github.com/getloopscale/loop-runtime/api/internal/teams/teamshttp"
	"github.com/getloopscale/loop-runtime/api/internal/tenants/tenantshttp"
	"github.com/getloopscale/loop-runtime/api/internal/timeline/timelinehttp"
	"github.com/getloopscale/loop-runtime/pkg/enforcement"
)

// Registration-only stubs: each embeds its interface, so it satisfies it
// while no method is ever called — these tests only mount routes.
type (
	stubSourceStore      struct{ source.SourceStore }
	stubProjectStore     struct{ ports.ProjectStore }
	stubTenantCRManager  struct{ ports.TenantCRManager }
	stubEventStore       struct{ sse.EventStore }
	stubBuildStoreReader struct{ sse.BuildStoreReader }
	stubSecretStore      struct{ secretshttp.SecretStore }
	stubSecretLocker     struct{ secretshttp.SecretEnvLocker }
	stubTenantDeletion   struct {
		tenantshttp.TenantDeletionStore
	}
	stubTimelineStore struct {
		timelinehttp.DeployTimelineEventStore
	}
	stubIdempotencyStore struct{ policy.IdempotencyStore }
)

// fullProtectedConfig mounts every protected route family.
func fullProtectedConfig() Config {
	return Config{
		TokenValidator:            fixedTokenValidator{},
		MemberStore:               fixedMembership{},
		SourceStore:               stubSourceStore{},
		ProjectStore:              stubProjectStore{},
		TenantCRMgr:               stubTenantCRManager{},
		EventStore:                stubEventStore{},
		BuildStoreReader:          stubBuildStoreReader{},
		BuildHandler:              &buildshttp.BuildHandler{},
		BuildAttestationHandler:   &buildshttp.BuildAttestationHandler{},
		BuildSBOMHandler:          &buildshttp.BuildSBOMHandler{},
		LogsHandler:               &logshttp.LogsHandler{},
		LogStreamHandler:          &logshttp.LogStreamHandler{},
		StatusHandler:             &statushttp.StatusHandler{},
		MetricsProxyHandler:       &projectmetricshttp.MetricsProxyHandler{},
		StatusSSEHandler:          &statushttp.StatusSSEHandler{},
		DeployTimelineStore:       stubTimelineStore{},
		ScaleHandler:              &scalinghttp.ScaleHandler{},
		BuildListHandler:          &buildshttp.BuildListHandler{},
		BuildStatsHandler:         &buildshttp.BuildStatsHandler{},
		RollbackHandler:           &rollbackhttp.RollbackHandler{},
		PromoteHandler:            &promotehttp.PromoteHandler{},
		TeamHandler:               &teamshttp.TeamHandler{},
		TenantDeletionStore:       stubTenantDeletion{},
		BillingHandler:            &billinghttp.BillingHandler{},
		EnvironmentHandler:        &environmentshttp.EnvironmentHandler{},
		DomainHandler:             &domainshttp.DomainHandler{},
		RepositoryHandler:         &repositorieshttp.RepositoryHandler{},
		IntegrationsGitHubHandler: &integrationshttp.IntegrationsGitHubHandler{},
		PreviewHandler:            &previewhttp.PreviewHandler{},
		AuditHandler:              &audithttp.AuditHandler{},
		Guards:                    RouteGuards{RateLimiter: &ratelimit.RateLimiter{}, VerifyRateLimiter: &ratelimit.VerifyRateLimiter{}, IdempotencyStore: stubIdempotencyStore{}, PlanLimiter: &enforcement.PlanLimiter{}, CostEnforcer: &policy.CostEnforcer{}},
		Secrets:                   SecretsConfig{Store: stubSecretStore{}, EnvLocker: stubSecretLocker{}},
	}
}

func routeSet(t *testing.T, r chi.Router) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	if err := chi.Walk(r, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		out[method+" "+pattern] = true
		return nil
	}); err != nil {
		t.Fatalf("walk: %v", err)
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// protectedFamily is one guarded branch of the protected route registration:
// the dependency that switches it, the surface it records, and exactly the
// routes it mounts.
type protectedFamily struct {
	surface string
	disable func(*Config)
	routes  []string
}

var protectedFamilies = []protectedFamily{
	{surfaces.SourceUpload, func(c *Config) { c.SourceStore = nil }, []string{
		"POST /api/v1/source/upload"}},
	{surfaces.Projects, func(c *Config) { c.ProjectStore = nil }, []string{
		"GET /api/v1/projects", "GET /api/v1/projects/{name}", "POST /api/v1/projects",
		"DELETE /api/v1/projects/{name}", "DELETE /api/v1/projects/{name}/environments/{env}"}},
	{surfaces.EventsStream, func(c *Config) { c.EventStore = nil }, []string{
		"GET /api/v1/builds/{id}/stream", "GET /api/v1/projects/{name}/builds/stream"}},
	{surfaces.Secrets, func(c *Config) { c.Secrets.EnvLocker = nil }, []string{
		"GET /api/v1/secrets", "POST /api/v1/secrets", "POST /api/v1/secrets/bulk",
		"DELETE /api/v1/secrets/{name}"}},
	{surfaces.Builds, func(c *Config) { c.BuildHandler = nil }, []string{
		"GET /api/v1/builds/{id}", "POST /api/v1/builds", "POST /api/v1/builds/{id}/cancel"}},
	{surfaces.BuildAttestation, func(c *Config) { c.BuildAttestationHandler = nil }, []string{
		"GET /api/v1/builds/{id}/attestation"}},
	{surfaces.BuildSBOM, func(c *Config) { c.BuildSBOMHandler = nil }, []string{
		"GET /api/v1/builds/{id}/sbom", "GET /api/v1/builds/{id}/sbom/{app}/download"}},
	{surfaces.BuildLogs, func(c *Config) { c.LogsHandler = nil }, []string{
		"GET /api/v1/builds/{id}/logs"}},
	{surfaces.LogStream, func(c *Config) { c.LogStreamHandler = nil }, []string{
		"GET /api/v1/builds/{id}/logs/stream", "GET /api/v1/projects/{name}/logs/stream"}},
	{surfaces.ProjectStatus, func(c *Config) { c.StatusHandler = nil }, []string{
		"GET /api/v1/projects/{name}/status"}},
	{surfaces.MetricsProxy, func(c *Config) { c.MetricsProxyHandler = nil }, []string{
		"GET /api/v1/projects/{name}/metrics"}},
	{surfaces.StatusStream, func(c *Config) { c.StatusSSEHandler = nil }, []string{
		"GET /api/v1/projects/{name}/signals/stream"}},
	{surfaces.Scale, func(c *Config) { c.ScaleHandler = nil }, []string{
		"POST /api/v1/projects/{name}/scale"}},
	{surfaces.BuildList, func(c *Config) { c.BuildListHandler = nil }, []string{
		"GET /api/v1/projects/{name}/builds", "GET /api/v1/projects/{name}/build-envs"}},
	{surfaces.BuildStats, func(c *Config) { c.BuildStatsHandler = nil }, []string{
		"GET /api/v1/builds/stats"}},
	{surfaces.Rollback, func(c *Config) { c.RollbackHandler = nil }, []string{
		"POST /api/v1/projects/{name}/rollback", "GET /api/v1/projects/{name}/rollbacks"}},
	{surfaces.Promote, func(c *Config) { c.PromoteHandler = nil }, []string{
		"POST /api/v1/projects/{name}/promote"}},
	{surfaces.Teams, func(c *Config) { c.TeamHandler = nil }, []string{
		"GET /api/v1/teams/members", "POST /api/v1/teams/members",
		"DELETE /api/v1/teams/members/{username}", "PATCH /api/v1/teams/members/{username}"}},
	{surfaces.TenantDeletion, func(c *Config) { c.TenantDeletionStore = nil }, []string{
		"DELETE /api/v1/tenants/{id}", "POST /api/v1/tenants/{id}/restore"}},
	{surfaces.Billing, func(c *Config) { c.BillingHandler = nil }, []string{
		"GET /api/v1/billing", "POST /api/v1/billing/portal"}},
	{surfaces.Environments, func(c *Config) { c.EnvironmentHandler = nil }, []string{
		"GET /api/v1/environments", "GET /api/v1/environments/{name}", "POST /api/v1/environments",
		"DELETE /api/v1/environments/{name}"}},
	{surfaces.Domains, func(c *Config) { c.DomainHandler = nil }, []string{
		"GET /api/v1/domains", "GET /api/v1/domains/{domain}", "GET /api/v1/domains/{domain}/dns-records", "POST /api/v1/domains",
		"POST /api/v1/domains/{domain}/verify", "DELETE /api/v1/domains/{domain}"}},
	{surfaces.Repositories, func(c *Config) { c.RepositoryHandler = nil }, []string{
		"POST /api/v1/projects/{name}/connect", "DELETE /api/v1/projects/{name}/connect",
		"GET /api/v1/projects/{name}/connect"}},
	{surfaces.IntegrationsGitHub, func(c *Config) { c.IntegrationsGitHubHandler = nil }, []string{
		"GET /api/v1/integrations/github/installations/{id}/repositories",
		"GET /api/v1/integrations/github/installations",
		"POST /api/v1/integrations/github/installations/claim"}},
	{surfaces.Previews, func(c *Config) { c.PreviewHandler = nil }, []string{
		"GET /api/v1/projects/{project}/previews", "GET /api/v1/projects/{project}/previews/{id}",
		"DELETE /api/v1/projects/{project}/previews/{id}", "POST /api/v1/projects/{project}/previews"}},
	{surfaces.Audit, func(c *Config) { c.AuditHandler = nil }, []string{
		"GET /api/v1/audit"}},
}

// Each guarded family mounts exactly its routes when its dependency is
// present, and when it is absent mounts none of them and records the
// surface as disabled with a reason. One case per branch of the protected
// route registration.
func TestProtectedRouteFamilies_MountOnlyWithTheirDependency(t *testing.T) {
	full := routeSet(t, NewRouter(fullProtectedConfig()))
	for _, fam := range protectedFamilies {
		t.Run(fam.surface, func(t *testing.T) {
			for _, rt := range fam.routes {
				if !full[rt] {
					t.Errorf("with its dependency, %s is not mounted", rt)
				}
			}
			cfg := fullProtectedConfig()
			fam.disable(&cfg)
			reg := surfaces.NewRegistry()
			cfg.Surfaces = reg
			without := routeSet(t, NewRouter(cfg))
			removed := map[string]bool{}
			for rt := range full {
				if !without[rt] {
					removed[rt] = true
				}
			}
			want := map[string]bool{}
			for _, rt := range fam.routes {
				want[rt] = true
			}
			if strings.Join(sortedKeys(removed), "\n") != strings.Join(sortedKeys(want), "\n") {
				t.Errorf("disabling %s removed:\n  %v\nwant:\n  %v", fam.surface, sortedKeys(removed), sortedKeys(want))
			}
			disabled := false
			for _, s := range reg.Disabled() {
				if s.Name == fam.surface && s.Reason != "" {
					disabled = true
				}
			}
			if !disabled {
				t.Errorf("surface %s not recorded as disabled with a reason", fam.surface)
			}
		})
	}
}

// The deploy timeline family records no surface; it mounts only with its store.
func TestProtectedRoutes_DeployTimelineMountsOnlyWithItsStore(t *testing.T) {
	full := routeSet(t, NewRouter(fullProtectedConfig()))
	cfg := fullProtectedConfig()
	cfg.DeployTimelineStore = nil
	without := routeSet(t, NewRouter(cfg))
	var removed []string
	for rt := range full {
		if !without[rt] {
			removed = append(removed, rt)
		}
	}
	if len(removed) == 0 {
		t.Fatal("removing the timeline store removed no route: the family is not guarded by it")
	}
	for _, rt := range removed {
		if !strings.Contains(rt, "timeline") {
			t.Errorf("removing the timeline store removed an unrelated route %s", rt)
		}
	}
}

// Every route the full protected configuration mounts belongs to a declared
// family, the timeline, or the always-on auth routes — so a new route cannot
// be added without being placed.
func TestProtectedRoutes_EveryMountedRouteIsDeclared(t *testing.T) {
	declared := map[string]bool{
		"GET /api/v1/auth/whoami": true,
	}
	for _, fam := range protectedFamilies {
		for _, rt := range fam.routes {
			declared[rt] = true
		}
	}
	var undeclared []string
	for rt := range routeSet(t, NewRouter(fullProtectedConfig())) {
		if !strings.Contains(rt, "/api/v1/") || strings.Contains(rt, "timeline") ||
			strings.Contains(rt, "/health") || strings.Contains(rt, "/version") ||
			strings.Contains(rt, "/admin/") || strings.Contains(rt, "/webhooks/") {
			continue
		}
		if !declared[rt] {
			undeclared = append(undeclared, rt)
		}
	}
	sort.Strings(undeclared)
	if len(undeclared) > 0 {
		t.Errorf("routes mounted but not declared in protectedFamilies:\n  %s", strings.Join(undeclared, "\n  "))
	}
}
