// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routeauthcoverage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Enumeration moved from a line regex to the AST, and the scope stopped being a
// single file — MEET-ASYNC-AMENDMENT-2026-08-17-route-auth-coverage-is-blind-to-40-percent.
//
// The regex required `r.` to be followed IMMEDIATELY by the verb, so
// `r.With(mw...).Post(...)` — 28 routes — did not match. The gate reported 42 over a
// system with ~71.

// tree writes a minimal tree with a router.go and, optionally, other files.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// routerWith builds a syntactically valid router.go with an Auth group containing
// `inside` and loose registrations in `outside`.
func routerWith(inside, outside string) string {
	return `package server

import "github.com/getloopscale/loopzero/api/internal/middleware"

func NewRouter(cfg Config) *chi.Mux {
	r := chi.NewRouter()
` + outside + `
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth)
` + inside + `
	})
	return r
}
`
}

// The shape the regex could not see. It is the most numerous one in the real router.
func TestWithChainedRegistrationIsEnumerated(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.With(rateLimit...).Post(\"/api/v1/builds\", h)\n", ""),
	})
	out, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1}))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.PassCount != 1 {
		t.Fatalf("`r.With(...).Post(` is a route registration and must be enumerated; PassCount=%d", out.PassCount)
	}
}

// And it must be enumerated ALSO when it is unprotected — otherwise the change
// would trade one blindness for another, counting only what already passes.
func TestWithChainedRegistrationOutsideAuthIsAViolation(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.Get(\"/api/v1/ok\", h)\n", "\tr.With(rateLimit...).Post(\"/api/v1/open\", h)\n"),
	})
	out, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("an `r.With(...)` route outside the Auth group and outside the allowlist must fail; err=%v out=%+v", err, out)
	}
	if len(out.Violations) != 1 || !strings.Contains(out.Violations[0], "/api/v1/open") {
		t.Fatalf("the violation must name the route; got %v", out.Violations)
	}
}

// The AST must not confuse text with code: a path quoted in a comment or in a
// string is not a route.
func TestCommentedRegistrationIsNotEnumerated(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.Get(\"/api/v1/ok\", h)\n",
			"\t// r.Post(\"/api/v1/ghost\", h) — turned off in 2026-01\n"),
	})
	out, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1}))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.PassCount != 1 {
		t.Fatalf("a commented-out registration is not a route; PassCount=%d", out.PassCount)
	}
}

// The instance the item measured: a route mounted by a helper, in another file, that
// the gate never saw. It does not try to decide whether the route is protected — it
// refuses the invisibility.
func TestRouteRegisteredOutsideRouterFileIsRefused(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.Get(\"/api/v1/ok\", h)\n", ""),
		"api/internal/routes/x/mount.go": `package x

func Register(r chi.Router) {
	r.Get("/api/v1/hidden", h)
}
`,
	})
	_, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1}))
	if !errors.Is(err, ErrUndeclaredOffRouterMount) {
		t.Fatalf("a registration outside router.go must fail until it is declared; err=%v", err)
	}
}

// Declared with a reason, it passes. The price of entering the list is writing down why.
func TestDeclaredOffRouterMountPasses(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.Get(\"/api/v1/ok\", h)\n", ""),
		"api/internal/routes/declared/mount.go": `package declared

func Register(r chi.Router) {
	r.Get("/api/v1/declared", h)
}
`,
	})
	cfg := Config{RepoRoot: root, MinRoutes: 1, OffRouterMounts: map[string]string{
		"api/internal/routes/declared/mount.go": "canonical mount point, called from inside the protected range",
	}}
	if _, err := violationOf(Check(cfg)); err != nil {
		t.Fatalf("a mount point declared with a reason must pass: %v", err)
	}
}

// An entry without a reason is not a declaration — it is a silencing.
func TestOffRouterMountWithoutReasonIsRefused(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.Get(\"/api/v1/ok\", h)\n", ""),
		"api/internal/routes/empty/mount.go": `package empty

func Register(r chi.Router) {
	r.Get("/api/v1/x", h)
}
`,
	})
	cfg := Config{RepoRoot: root, MinRoutes: 1, OffRouterMounts: map[string]string{
		"api/internal/routes/empty/mount.go": "   ",
	}}
	if _, err := violationOf(Check(cfg)); !errors.Is(err, ErrUndeclaredOffRouterMount) {
		t.Fatalf("a declaration without a reason declares nothing; err=%v", err)
	}
}

// A test is not production. Counting `_test.go` would inflate the floor and turn a
// fixture into public surface.
func TestRegistrationInsideATestFileIsIgnored(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.Get(\"/api/v1/ok\", h)\n", ""),
		"api/internal/routes/x/mount_test.go": `package x

func TestX(t *testing.T) {
	r.Get("/api/v1/fixture", h)
}
`,
	})
	if _, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1})); err != nil {
		t.Fatalf("a registration in a test file is not public surface: %v", err)
	}
}

// The floor: refusing only zero lets a drop by half through, and that is the realistic
// failure mode. `MinRoutes` is a MINIMUM count — not the exact one, which would become
// maintenance on every new route and be switched off by the first person to trip on it.
func TestEnumerationCollapseBelowFloorFails(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.Get(\"/api/v1/ok\", h)\n", ""),
	})
	_, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 10}))
	if !errors.Is(err, ErrEnumerationBelowFloor) {
		t.Fatalf("1 route under a floor of 10 is a collapse, not a small system; err=%v", err)
	}
}

// The opposite boundary of the floor: exactly at the minimum passes. Without this,
// tightening the floor would become a rule the repository itself does not satisfy.
func TestExactlyAtTheFloorPasses(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.Get(\"/a\", h)\n\t\tr.Get(\"/b\", h)\n", ""),
	})
	if _, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 2})); err != nil {
		t.Fatalf("exactly at the floor must pass: %v", err)
	}
}

// A router.go that does not parse is an instrumentation error, and it must say so —
// not enumerate zero and land on an error that talks about something else.
func TestUnparseableRouterIsAnInstrumentationError(t *testing.T) {
	root := tree(t, map[string]string{routerPath: "package server\nfunc broken( {\n"})
	_, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1}))
	if !errors.Is(err, ErrRouterUnparseable) {
		t.Fatalf("an invalid router.go must fail as a parse error; err=%v", err)
	}
}

// The real population (B-109). A synthetic mutant proves the detector fires; only
// this proves what it sees in what exists — and it is the assertion that pins the fix,
// because the count had to RISE from 42 to ~71.
func TestRealRouterEnumeratesTheChainedFormToo(t *testing.T) {
	out, err := violationOf(Check(Config{RepoRoot: repoRoot}))
	if err != nil {
		t.Fatalf("Check on the real tree: %v", err)
	}
	if out.PassCount < 60 {
		t.Fatalf("PassCount=%d — the real router has ~42 direct registrations PLUS ~28 in the `r.With(...)` shape; "+
			"a number close to 42 means the chained shape became invisible again", out.PassCount)
	}
}
