// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routeauthcoverage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "api", "internal", "server")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "router.go"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root
}

// canonicalRouter mirrors the real router.go shape: public routes at the top,
// an Auth group in the middle, a registerProtectedRoutes helper called from
// inside the Auth group.
const canonicalRouter = `package server

import (
	"github.com/go-chi/chi/v5"
	"github.com/getloopscale/loopzero/api/internal/middleware"
)

func NewRouter() *chi.Mux {
	r := chi.NewRouter()

	// Public routes (allowlisted)
	r.Get("/api/v1/health", healthHandler)
	r.Get("/api/v1/health/deep", healthDeepHandler)
	r.Get("/api/v1/version", versionHandler)
	r.Post("/api/v1/webhooks/github", githubWebhookHandler)
	r.Post("/internal/bootstrap", bootstrapHandler)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth)
		registerProtectedRoutes(r)
		r.Get("/api/v1/whoami", whoamiHandler)
		r.Post("/api/v1/deploy", deployHandler)
	})

	return r
}

func registerProtectedRoutes(r chi.Router) {
	r.Get("/api/v1/projects", listProjectsHandler)
	r.Post("/api/v1/projects", createProjectHandler)
	r.Delete("/api/v1/projects/{id}", deleteProjectHandler)
}
`

func TestCheck_CanonicalPasses(t *testing.T) {
	root := writeFixture(t, canonicalRouter)
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("expected PASS, got err=%v violations=%v", err, outcome.Violations)
	}
	if outcome.PassCount == 0 {
		t.Fatalf("expected some routes counted PASS, got 0")
	}
	if outcome.AuthStart == 0 || outcome.AuthEnd == 0 {
		t.Fatalf("Auth group not located (start=%d end=%d)", outcome.AuthStart, outcome.AuthEnd)
	}
	if outcome.ProtStart == 0 || outcome.ProtEnd == 0 {
		t.Fatalf("registerProtectedRoutes not located (start=%d end=%d)", outcome.ProtStart, outcome.ProtEnd)
	}
}

func TestCheck_MissingRouterFails(t *testing.T) {
	root := t.TempDir()
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrRouterNotFound) {
		t.Fatalf("expected ErrRouterNotFound, got %v", err)
	}
}

func TestCheck_MissingAuthGroupFails(t *testing.T) {
	// Router with NO Auth group.
	body := `package server

import "github.com/go-chi/chi/v5"

func NewRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Get("/api/v1/foo", fooHandler)
	return r
}
`
	root := writeFixture(t, body)
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrAuthGroupMissing) {
		t.Fatalf("expected ErrAuthGroupMissing, got %v", err)
	}
}

func TestCheck_UnprotectedRouteFails(t *testing.T) {
	// Add a route OUTSIDE the Auth group AND not in allowlist.
	body := canonicalRouter + `
func extra() {
	r := chi.NewRouter()
	r.Get("/api/v1/leaked-route", leakedHandler)
}
`
	root := writeFixture(t, body)
	outcome, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for unprotected route, got %v (violations=%v)", err, outcome.Violations)
	}
	if len(outcome.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %v", len(outcome.Violations), outcome.Violations)
	}
}

func TestCheck_AllowlistPrefixMatch(t *testing.T) {
	// Route /api/v1/admin/releases/{id}/inspect matches /api/v1/admin/releases prefix.
	body := canonicalRouter + `
func adminExtra() {
	r := chi.NewRouter()
	r.Get("/api/v1/admin/releases/{id}/inspect", inspectHandler)
	r.Get("/api/v1/admin/tenants/pin", pinHandler)
	r.Get("/internal/v1/secret-fetch", secretFetchHandler)
}
`
	root := writeFixture(t, body)
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("expected PASS with allowlist prefix match, got err=%v violations=%v", err, outcome.Violations)
	}
}

func TestCheck_RealRepoRouterPasses(t *testing.T) {
	abs, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, statErr := os.Stat(filepath.Join(abs, routerPath)); statErr == nil {
			outcome, checkErr := Check(Config{RepoRoot: abs})
			if checkErr != nil {
				t.Fatalf("real router.go must satisfy invariant. err=%v violations=%v", checkErr, outcome.Violations)
			}
			return
		}
		abs = filepath.Dir(abs)
	}
	t.Skip("could not locate api/internal/server/router.go within 8 parent dirs")
}
