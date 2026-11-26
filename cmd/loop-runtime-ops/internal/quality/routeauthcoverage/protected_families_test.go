// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package routeauthcoverage

import (
	"errors"
	"strings"
	"testing"
)

// registerProtectedRoutes moved out of router.go into protected_routes.go and
// now reaches its routes through per-family registrars listed in a slice. The
// gate must follow it there: the routes are protected because the Auth group
// calls registerProtectedRoutes, and they must be counted — not dropped below
// the floor, and not reported as undeclared off-router mounts.
const protectedFamiliesFile = `package server

var protectedRouteFamilies = []protectedRouteFamily{
	registerBuildRoutes,
	registerTeamRoutes,
}

func registerProtectedRoutes(r chi.Router, cfg Config, ch routeChains) {
	for _, register := range protectedRouteFamilies {
		register(r, cfg, ch)
	}
	registerAuditRoutes(r, cfg, ch)
}

func registerBuildRoutes(r chi.Router, cfg Config, ch routeChains) {
	r.With(ch.limited()...).Post("/api/v1/builds", h)
	r.Get("/api/v1/builds/{id}", h)
}

func registerTeamRoutes(r chi.Router, cfg Config, ch routeChains) {
	r.Get("/api/v1/teams/members", h)
}

func registerAuditRoutes(r chi.Router, cfg Config, _ routeChains) {
	r.Get("/api/v1/audit", h)
}
`

func TestProtectedFamiliesInAnotherFileAreCountedAsProtected(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tregisterProtectedRoutes(r, cfg, chains)\n", ""),
		"api/internal/server/protected_routes.go": protectedFamiliesFile,
	})

	out, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 4, OffRouterMounts: map[string]string{}}))

	if err != nil {
		t.Fatalf("Check: %v (violations %v, off-router %v)", err, out.Violations, out.OffRouterUndeclared)
	}
	if out.PassCount != 4 {
		t.Errorf("PassCount = %d, want 4 (two family registrars, one direct call)", out.PassCount)
	}
}

func TestProtectedFamiliesAreViolationsWhenNotCalledFromTheAuthGroup(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("", "\tregisterProtectedRoutes(r, cfg, chains)\n"),
		"api/internal/server/protected_routes.go": protectedFamiliesFile,
	})

	out, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1, OffRouterMounts: map[string]string{}}))

	if !errors.Is(err, ErrViolations) {
		t.Fatalf("err = %v, want ErrViolations: registerProtectedRoutes runs outside the Auth group", err)
	}
	if len(out.Violations) != 4 || !strings.Contains(strings.Join(out.Violations, "\n"), "/api/v1/audit") {
		t.Errorf("violations = %v, want the four family routes", out.Violations)
	}
}
