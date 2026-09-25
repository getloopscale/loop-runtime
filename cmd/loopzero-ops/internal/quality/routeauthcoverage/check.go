// Package routeauthcoverage enforces R2-F8 (HIGH) from the 49-findings code review:
// every user-facing API route in api/internal/server/router.go MUST be registered
// INSIDE the middleware.Auth group OR appear in the explicit public-allowlist.
//
// New routes added without auth + without allowlist entry fail this gate.
//
// Algorithm, over the go/parser AST of router.go:
//  1. Locate the body of the `Group(func(r chi.Router){…})` that references
//     middleware.Auth, by AST position.
//  2. Locate the body of registerProtectedRoutes (also protected).
//  3. Enumerate every chi registration call (Get, Post, …, Handle, Method), on any
//     receiver chain, so `r.With(mw).Post(…)` counts like `r.Post(…)`.
//  4. For each: PASS if its line is inside either body OR the route matches an
//     allowlist prefix. FAIL otherwise.
//
// It then refuses an enumeration below the declared floor, and scans the rest of
// the api tree for route registrations outside router.go that are not declared.
// Hermetic: it parses source and runs nothing.
package routeauthcoverage

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

const routerPath = "api/internal/server/router.go"

// allowlist mirrors the bash guard's ALLOWLIST array (order preserved).
// Prefix-match against the route string.
var allowlist = []string{
	"/api/v1/health",
	"/api/v1/health/deep",
	"/api/v1/version",
	// Status-fleet contract. PUBLIC for the same reason as /api/v1/version just
	// above — it serves the SAME build metadata, which is not a secret — and for a
	// reason of its own: the /status panel's collector probes the target with no
	// session at all. Behind Auth, the panel would read 401 as "service
	// unavailable", which is precisely the lie it exists to avoid.
	// It exposes nothing beyond {version, built_at}.
	"/v1/version",
	"/internal/bootstrap",
	"/internal/argocd-callback",
	"/internal/v1/secret-fetch",
	"/api/v1/webhooks/github",
	"/api/v1/admin/tenants/evict",
	"/api/v1/admin/tenants/pin",
	"/api/v1/admin/releases",
}

// Config controls the check.
type Config struct {
	RepoRoot string
	// OffRouterMounts declares production files that register a route outside
	// router.go, mapping path → REASON. Nil uses DefaultOffRouterMounts.
	//
	// The gate does not decide whether those routes are protected — it refuses not
	// to see them. An empty (non-nil) map means "no declarations", which is what
	// the refusal tests exercise.
	OffRouterMounts map[string]string
	// MinRoutes is the MINIMUM count of enumerated routes. Zero uses DefaultMinRoutes.
	//
	// Minimum and not exact on purpose: an exact number would become maintenance on
	// every new route, and the first person to trip on it would switch it off.
	MinRoutes int
}

// DefaultOffRouterMounts is the real list, and today it has exactly one entry.
//
// Measured on 2026-08-17 by scanning production `api/**/*.go`: a single file registers
// a route outside router.go.
var DefaultOffRouterMounts = map[string]string{
	"api/internal/timeline/timelinehttp/deploy_timeline_register.go": "canonical mount point of the timeline, called from router.go:550 — from INSIDE the protected range. " +
		"The helper exists so the wiring has a test (`deploy_timeline_register_test.go`), and the route returns its own 401 when there is no tenant. " +
		"Measured in B-123: the gate reported 42 routes over a system with at least 43, and this was the missing one.",
}

// DefaultMinRoutes is the floor. The real router enumerates ~71; 55 leaves room for
// the legitimate removal of a family (B-080 is moving routes) and still catches a drop
// by half, which is the realistic failure mode.
const DefaultMinRoutes = 55

// Outcome is the summary.
type Outcome struct {
	AuthStart, AuthEnd int
	ProtStart, ProtEnd int
	PassCount          int
	Violations         []string
	// OffRouterUndeclared lists registrations outside router.go with no declaration.
	OffRouterUndeclared []string
}

// ErrViolations means at least one route is unprotected and un-allowlisted.
var ErrViolations = errors.New("at least one router.go route lacks middleware.Auth protection and lacks an allowlist entry")

// ErrRouterNotFound is emitted when api/internal/server/router.go is missing.
var ErrRouterNotFound = errors.New("router.go not found")

// ErrNoRoutesEnumerated means the walker found the structural markers but no
// route registrations at all. A coverage gate that examined nothing has not
// passed — it has abstained, and reporting that as PASS is how the auth
// invariant over the whole public surface would disappear in silence during the
// routes split (B-040, B-041).
var ErrNoRoutesEnumerated = errors.New("router.go parsed and the Auth group located, but ZERO route registrations were enumerated — the gate examined nothing, so it cannot vouch for anything")

// ErrAuthGroupMissing is emitted when the Auth chi.Router group cannot be
// located — that itself is a systemic regression the gate refuses to gloss over.
var ErrAuthGroupMissing = errors.New("could not locate the middleware.Auth chi.Router group in router.go")

// Check runs the router.go coverage audit.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	full := filepath.Join(root, routerPath)
	if _, statErr := os.Stat(full); statErr != nil {
		return Outcome{}, ErrRouterNotFound
	}
	fset := token.NewFileSet()
	file, perr := parser.ParseFile(fset, full, nil, parser.SkipObjectResolution)
	if perr != nil {
		// BOTH errors are wrapped: `errors.Is` still recognizes the sentinel, and
		// whoever reads the log gets the exact parse position — which is the whole
		// diagnosis when a gate says "I did not understand the file".
		return Outcome{}, fmt.Errorf("%w: %w", ErrRouterUnparseable, perr)
	}

	authStart, authEnd, protStart, protEnd, authCallsProtected := authRanges(fset, file)
	if authStart == 0 {
		return Outcome{}, ErrAuthGroupMissing
	}

	pass := 0
	var violations []string

	// registerProtectedRoutes may live outside router.go and reach its routes
	// through family registrars; follow it. Its routes are protected exactly
	// when the Auth group is what calls it.
	protected, protectedSites, perr := protectedFunctions(root)
	if perr != nil {
		return Outcome{}, fmt.Errorf("server package: %w", perr)
	}
	for _, site := range protectedSites {
		if site.File == routerPath {
			continue // enumerated below with router.go's own ranges
		}
		if authCallsProtected {
			pass++
			continue
		}
		violations = append(violations, fmt.Sprintf("%s:%d  %s  (registered by %s, which the Auth group does not call)",
			site.File, site.Line, site.Pattern, protectedEntry))
	}

	for _, site := range enumerateFile(fset, file, routerPath) {
		route := site.Pattern
		lineNo := site.Line

		// Inside Auth group
		if lineNo >= authStart && lineNo <= authEnd {
			pass++
			continue
		}
		// Inside registerProtectedRoutes
		if protStart > 0 && lineNo >= protStart && lineNo <= protEnd {
			pass++
			continue
		}
		// Allowlist prefix match
		if inAllowlist(route) {
			pass++
			continue
		}
		violations = append(violations, fmt.Sprintf("router.go:%d  %s  (NOT in Auth group, NOT in allowlist)", lineNo, route))
	}

	outcome := Outcome{
		AuthStart:  authStart,
		AuthEnd:    authEnd,
		ProtStart:  protStart,
		ProtEnd:    protEnd,
		PassCount:  pass,
		Violations: violations,
	}
	if len(violations) > 0 {
		return outcome, ErrViolations
	}
	// The enumeration floor (B-040). Order matters and is load-bearing: this
	// runs AFTER ErrViolations, and it counts routes ENUMERATED — approved plus
	// violating — not routes approved.
	//
	// Written over PassCount alone it would be wrong in the worst possible
	// direction. PassCount counts only APPROVED routes, so it is also zero when
	// EVERY route violates; the gate would then answer "enumerated nothing" — an
	// instrumentation error — for the most serious finding this repository can
	// produce, a public surface with no authentication. That was edge case EC-1 of
	// the slice-1 tripwires edge-case review (2026-08-11).
	enumerated := outcome.PassCount + len(violations)
	if enumerated == 0 {
		return outcome, ErrNoRoutesEnumerated
	}

	// The floor as a MINIMUM COUNT. `== 0` refused only the total collapse; a drop
	// from 71 to 35 passed, and that is the drop a change in registration shape
	// produces. It runs after ErrViolations for the same reason the zero check did:
	// enumerating too little is an instrumentation error, and a real security
	// finding takes precedence over it.
	floor := cfg.MinRoutes
	if floor == 0 {
		floor = DefaultMinRoutes
	}
	if enumerated < floor {
		return outcome, fmt.Errorf("%w: %d enumerated, floor %d", ErrEnumerationBelowFloor, enumerated, floor)
	}

	// The tree scan. It runs last: an unprotected route in router.go is a bigger
	// finding than an undeclared mount point.
	declared := cfg.OffRouterMounts
	if declared == nil {
		declared = DefaultOffRouterMounts
	}
	undeclared, serr := scanOffRouter(root, declared, protected)
	if serr != nil {
		return outcome, fmt.Errorf("tree scan: %w", serr)
	}
	if len(undeclared) > 0 {
		outcome.OffRouterUndeclared = undeclared
		return outcome, ErrUndeclaredOffRouterMount
	}
	return outcome, nil
}

// inAllowlist returns true when the route is exactly an allowed prefix or a
// path under an allowed prefix.
func inAllowlist(route string) bool {
	for _, a := range allowlist {
		if route == a || strings.HasPrefix(route, a+"/") {
			return true
		}
	}
	return false
}
