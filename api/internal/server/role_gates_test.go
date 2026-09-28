// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/middleware/ratelimit"
)

// roleMembership authenticates every request as a member of tn-fixed with one
// role, so the router's role gates can be exercised per role.
type roleMembership struct{ role string }

func (m roleMembership) GetMemberships(context.Context, string) ([]auth.TenantMembership, error) {
	return []auth.TenantMembership{{TenantID: "tn-fixed", Role: m.role}}, nil
}

func routerAs(t *testing.T, role string) http.Handler {
	t.Helper()
	cfg := fullProtectedConfig()
	cfg.MemberStore = roleMembership{role: role}
	cfg.Guards.RateLimiter = ratelimit.NewRateLimiter(1000, time.Minute)
	cfg.Guards.IdempotencyStore = memoryIdempotencyStore{}
	cfg.ActiveTeamStore = stubActiveTeamStore{}
	cfg.Guards.VerifyRateLimiter = nil
	cfg.AuditWriter = func(string, string, string, string, string, string, map[string]any) {}
	return NewRouter(cfg)
}

// The routes a member may not call, and the least role that may. One rule in
// one place (auth.RequireRole at registration) instead of an inline check per
// handler with its own error shape.
var roleGatedRoutes = map[string]string{
	"POST /api/v1/projects/{name}/connect":    "admin",
	"DELETE /api/v1/projects/{name}/connect":  "admin",
	"POST /api/v1/domains":                    "admin",
	"DELETE /api/v1/domains/{domain}":         "admin",
	"GET /api/v1/audit":                       "admin",
	"POST /api/v1/teams/members":              "admin",
	"DELETE /api/v1/teams/members/{username}": "admin",
	"PATCH /api/v1/teams/members/{username}":  "owner",
}

func TestRoleGates_RefuseARoleBelowTheMinimum(t *testing.T) {
	below := map[string]string{"admin": "member", "owner": "admin"}
	for route, minRole := range roleGatedRoutes {
		t.Run(route, func(t *testing.T) {
			rec := serveRecovering(routerAs(t, below[minRole]), mutationRequest(route, true))
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "INSUFFICIENT_ROLE") {
				t.Fatalf("%s as %s: want 403 INSUFFICIENT_ROLE, got %d body=%s", route, below[minRole], rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRoleGates_AdmitTheMinimumRole(t *testing.T) {
	for route, minRole := range roleGatedRoutes {
		t.Run(route, func(t *testing.T) {
			rec := serveRecovering(routerAs(t, minRole), mutationRequest(route, true))
			if strings.Contains(rec.Body.String(), "INSUFFICIENT_ROLE") {
				t.Fatalf("%s as %s: refused by the role gate: %s", route, minRole, rec.Body.String())
			}
		})
	}
}
