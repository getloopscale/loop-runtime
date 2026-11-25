// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	mwaudit "github.com/getloopscale/loopzero/api/internal/middleware/audit"
	ratelimit "github.com/getloopscale/loopzero/api/internal/middleware/ratelimit"
)

// auditIdentity is what the audit row records for a mutation: a stable
// action name and the type of resource it acted on.
type auditIdentity struct{ action, resourceType string }

// wantAuditIdentity is the audit vocabulary, route by route. Every mutating
// protected route must appear here: the identity is declared where the route
// is registered, so it can never be inferred wrongly from URL segments (the
// segment heuristic recorded POST /projects/{name}/scale as project.created).
var wantAuditIdentity = map[string]auditIdentity{
	"POST /api/v1/source/upload":                           {"source.uploaded", "source"},
	"POST /api/v1/projects":                                {"project.created", "project"},
	"DELETE /api/v1/projects/{name}":                       {"project.deleted", "project"},
	"DELETE /api/v1/projects/{name}/environments/{env}":    {"environment.deleted", "environment"},
	"POST /api/v1/secrets":                                 {"secret.set", "secret"},
	"POST /api/v1/secrets/bulk":                            {"secret.set", "secret"},
	"DELETE /api/v1/secrets/{name}":                        {"secret.deleted", "secret"},
	"POST /api/v1/builds":                                  {"build.submitted", "build"},
	"POST /api/v1/builds/{id}/cancel":                      {"build.cancelled", "build"},
	"POST /api/v1/projects/{name}/scale":                   {"project.scaled", "project"},
	"POST /api/v1/projects/{name}/rollback":                {"rollback.executed", "release"},
	"POST /api/v1/projects/{name}/promote":                 {"release.promoted", "release"},
	"POST /api/v1/teams/members":                           {"member.added", "member"},
	"DELETE /api/v1/teams/members/{username}":              {"member.removed", "member"},
	"PATCH /api/v1/teams/members/{username}":               {"member.role_changed", "member"},
	"DELETE /api/v1/tenants/{id}":                          {"tenant.deleted", "tenant"},
	"POST /api/v1/tenants/{id}/restore":                    {"tenant.restored", "tenant"},
	"POST /api/v1/billing/portal":                          {"billing.portal_opened", "billing"},
	"POST /api/v1/environments":                            {"environment.created", "environment"},
	"DELETE /api/v1/environments/{name}":                   {"environment.deleted", "environment"},
	"POST /api/v1/domains":                                 {"domain.added", "domain"},
	"POST /api/v1/domains/{domain}/verify":                 {"domain.verified", "domain"},
	"DELETE /api/v1/domains/{domain}":                      {"domain.removed", "domain"},
	"POST /api/v1/projects/{name}/connect":                 {"repository.connected", "repository"},
	"DELETE /api/v1/projects/{name}/connect":               {"repository.disconnected", "repository"},
	"POST /api/v1/integrations/github/installations/claim": {"github_installation.claimed", "github_installation"},
	"POST /api/v1/projects/{project}/previews":             {"preview.created", "preview"},
	"DELETE /api/v1/projects/{project}/previews/{id}":      {"preview.deleted", "preview"},
	"POST /api/v1/auth/team/switch":                        {"team.switched", "team"},
}

// Each mutating route records exactly its declared identity, through the
// real router and the real audit middleware.
func TestMutatingRoutes_RecordTheirDeclaredAuditIdentity(t *testing.T) {
	routes := mutatingProtectedRoutes(t)
	if len(routes) != len(wantAuditIdentity) {
		t.Errorf("router has %d mutating routes, the audit vocabulary %d: every route needs an entry",
			len(routes), len(wantAuditIdentity))
	}
	for _, rt := range routes {
		want, ok := wantAuditIdentity[rt]
		if !ok {
			t.Errorf("%s has no audit identity in wantAuditIdentity", rt)
			continue
		}
		var got auditIdentity
		cfg := guardedConfigForWalk()
		cfg.Guards.IdempotencyStore = memoryIdempotencyStore{}
		cfg.Guards.RateLimiter = ratelimit.NewRateLimiter(1000, time.Hour)
		cfg.Guards.VerifyRateLimiter = nil
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
		cfg.AuditWriter = func(_, action, _, resourceType, _, _ string, _ map[string]any) {
			got = auditIdentity{action, resourceType}
		}
		inertRoute(t, NewRouter(cfg), rt).ServeHTTP(httptest.NewRecorder(), mutationRequest(rt, true))
		if got != want {
			t.Errorf("%s recorded %+v, want %+v", rt, got, want)
		}
	}
}

// inertRoute returns the route's REAL middleware chain (auth, audit, rate
// limit, idempotency, declarations — exactly what chi mounted) around a
// handler that only answers 200. The registration-only handlers of the full
// config would panic, and a panic unwinds past the audit write.
func inertRoute(t *testing.T, r chi.Router, route string) http.Handler {
	t.Helper()
	var chain []func(http.Handler) http.Handler
	found := false
	_ = chi.Walk(r, func(method, pattern string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		if method+" "+pattern == route {
			chain, found = mws, true
		}
		return nil
	})
	if !found {
		t.Fatalf("route %s not mounted", route)
	}
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	for i := len(chain) - 1; i >= 0; i-- {
		h = chain[i](h)
	}
	// chi's URL params are set by the mux; a direct call needs a route context.
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rctx := chi.NewRouteContext()
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		h.ServeHTTP(w, req)
	})
}

// A mutation that reaches the audit middleware without a declaration is
// recorded under a bounded name — never one carrying a path segment or id.
func TestAuditLog_UndeclaredMutationIsRecordedUnderABoundedName(t *testing.T) {
	var action, resourceType string
	w := mwaudit.AuditWriter(func(_, a, _, rt, _, _ string, _ map[string]any) { action, resourceType = a, rt })
	h := mwaudit.AuditLog(slog.New(slog.NewTextHandler(io.Discard, nil)), w)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	h.ServeHTTP(httptest.NewRecorder(), withFixedUser(httptest.NewRequest(http.MethodPost, "/api/v1/things/tn-123/frob", nil)))

	if action != "undeclared.POST" || resourceType != "undeclared" {
		t.Fatalf("got action=%q type=%q, want undeclared.POST / undeclared", action, resourceType)
	}
	if strings.Contains(action, "tn-123") || strings.Contains(action, "frob") {
		t.Fatalf("an undeclared action must not carry path segments: %q", action)
	}
}
