// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routeauthcoverage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
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
	outcome, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1}))
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
	_, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1}))
	if !errors.Is(err, ErrRouterNotFound) {
		t.Fatalf("expected ErrRouterNotFound, got %v", err)
	}
}

func TestCheck_MissingAuthGroupFails(t *testing.T) {
	// Router with NO Auth group.
	body := `package server

import (
	"github.com/go-chi/chi/v5"
	"github.com/getloopscale/loopzero/api/internal/middleware"
)

func NewRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Get("/api/v1/foo", fooHandler)
	return r
}
`
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1}))
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
	outcome, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1}))
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
	outcome, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1}))
	if err != nil {
		t.Fatalf("expected PASS with allowlist prefix match, got err=%v violations=%v", err, outcome.Violations)
	}
}

func TestCheck_RealRepoRouterPasses(t *testing.T) {
	abs := qtestutil.RootFile(t, routerPath)
	outcome, checkErr := violationOf(Check(Config{RepoRoot: abs}))
	if checkErr != nil {
		t.Fatalf("real router.go must satisfy invariant. err=%v violations=%v", checkErr, outcome.Violations)
	}
}

// --- B-040: the enumeration floor -----------------------------------------

// synthRouter writes a router.go that keeps the STRUCTURAL markers the walker
// keys on (the middleware.Auth group and registerProtectedRoutes) and lets the
// caller decide what lives inside them. That is the shape B-041 produces when
// the registrations move into per-domain Register() funcs.
func synthRouter(t *testing.T, insideAuth, insideProtected, outsideBoth string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "api", "internal", "server")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package server\n\nimport \"github.com/getloopscale/loopzero/api/internal/middleware\"\n\n" +
		"func Router() {\n" +
		"\tr.Group(func(r chi.Router) {\n" +
		"\t\tr.Use(middleware.Auth(cfg.TokenValidator, cfg.MemberStore))\n" +
		insideAuth +
		"\t})\n}\n\n" +
		"func registerProtectedRoutes(r chi.Router) {\n" +
		insideProtected +
		"}\n\n" +
		"func publicRoutes(r chi.Router) {\n" + outsideBoth + "}\n"
	if err := os.WriteFile(filepath.Join(dir, "router.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// The defect this closes: with the markers present and every registration moved
// out, the walker enumerates nothing, finds no violation, and reports success.
func TestCheck_ZeroRoutesEnumerated_IsAFailure(t *testing.T) {
	root := synthRouter(t,
		"\t\tprojects.Register(r, cfg)\n",
		"\tbuilds.Register(r, cfg)\n", "")

	out, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1}))
	if !errors.Is(err, ErrNoRoutesEnumerated) {
		t.Fatalf("a router with the markers and zero registrations must FAIL, not pass vacuously; got err=%v outcome=%+v", err, out)
	}
}

// EC-1 — the floor must be about ENUMERATION, not approval. PassCount counts
// only APPROVED routes, so it is also zero when every route violates. Written
// over PassCount alone, the gate would report "enumerated nothing" — an
// instrumentation error — for the worst finding the repository can produce:
// a public surface with no authentication.
func TestCheck_AllRoutesViolating_ReportsViolationsNotEmptyEnumeration(t *testing.T) {
	root := synthRouter(t,
		"\t\t// nothing inside the Auth group\n",
		"\t// nothing protected either\n",
		"\tr.Get(\"/api/v1/alpha\", h)\n\tr.Post(\"/api/v1/beta\", h)\n\tr.Delete(\"/api/v1/gamma\", h)\n")

	out, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1}))
	if errors.Is(err, ErrNoRoutesEnumerated) {
		t.Fatalf("BUG: %d unprotected routes reported as an empty enumeration — the diagnosis points at the gate instead of at the unprotected surface", len(out.Violations))
	}
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations; got %v (outcome=%+v)", err, out)
	}
	if out.PassCount != 0 || len(out.Violations) == 0 {
		t.Errorf("setup did not produce the intended state (PassCount=0, violations>0); got %+v", out)
	}
}

// The other side of the 0/1 boundary: one enumerated route is enough.
func TestCheck_SingleRouteEnumerated_Passes(t *testing.T) {
	root := synthRouter(t, "\t\tr.Get(\"/api/v1/only\", h)\n", "", "")

	out, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1}))
	if err != nil {
		t.Fatalf("a single approved route must pass; got %v", err)
	}
	if out.PassCount != 1 {
		t.Errorf("expected PassCount=1; got %d", out.PassCount)
	}
}

// Declared by the plan (T1.1) and missing until the review caught it: the floor
// must not change the verdict on the tree that actually ships. The acceptance
// criterion that would have caught the omission — `go test -run '...|RealTree'`
// — passed vacuously, because no test name matched.
func TestCheck_RealTreeStillPasses(t *testing.T) {
	root := qtestutil.Root(t)
	out, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1}))
	if err != nil {
		t.Fatalf("the shipping tree must still pass; got %v", err)
	}
	// 70, not the 42 measured on 2026-08-11 — and the change is DELIBERATE, from B-123.
	//
	// The enumerator was a regex that required `r.` immediately followed by the verb, so
	// `r.With(rateLimitMiddlewares...).Post(…)` did not match. There are **28** routes in
	// that shape. The gate reported 42 over a system with ~71 and sold "every public
	// route is behind Auth" while checking 59% of them.
	//
	// The count had to RISE, and this number is the proof that it did: 42 + 28 = 70.
	// The 71st is the one mounted by a helper in another file, counted by the tree scan
	// and not by this enumeration.
	//
	// If this number falls back near 42, the chained shape became invisible again.
	//
	// 2026-09-29: 43 direct — GET /internal/edge/route joined the internal routes (#447).
	//
	// 2026-11-25: 44 direct + 29 chained. Two routes of the GitHub integration joined
	// the Auth group: GET /integrations/github/installations (direct) and
	// POST /integrations/github/installations/claim (rate limited, so chained).
	// 2026-11-25, later the same day: 45 direct. GET /api/v1/projects/{name}/rollbacks
	// lists the recorded rollbacks of a project.
	// 2026-11-26: 46 direct. GET /api/v1/builds/{id}/sbom/{app}/download serves the SBOM
	// file that a presigned storage URL served before.
	if out.PassCount != 75 {
		t.Errorf("expected the 75 routes the AST enumerator sees (46 direct + 29 in the `r.With(...)` shape, measured 2026-11-26); got %d — if this changed on purpose, update the number and say why", out.PassCount)
	}
}
