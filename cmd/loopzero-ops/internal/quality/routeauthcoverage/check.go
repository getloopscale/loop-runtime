// Package routeauthcoverage enforces R2-F8 (HIGH) from the 49-findings code review:
// every user-facing API route in api/internal/server/router.go MUST be registered
// INSIDE the middleware.Auth group OR appear in the explicit public-allowlist.
//
// New routes added without auth + without allowlist entry fail this gate.
//
// Algorithm (ports the bash awk brace-counter into a Go line-walker):
//  1. Locate the middleware.Auth chi.Router group line range (via brace depth).
//  2. Locate the registerProtectedRoutes function body line range (also protected).
//  3. Enumerate every r.{Get,Post,Put,Patch,Delete,Method}( call.
//  4. For each: PASS if line inside Auth range OR ProtectedFunc range OR route
//     matches allowlist prefix. FAIL otherwise.
//
// Migrated from infra/tests/check-route-auth-coverage_test.sh
// (M-Migrate2 continuation 2026-07-07). Hermetic — pure text walk on router.go.
package routeauthcoverage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const routerPath = "api/internal/server/router.go"

// allowlist mirrors the bash guard's ALLOWLIST array (order preserved).
// Prefix-match against the route string.
var allowlist = []string{
	"/api/v1/health",
	"/api/v1/health/deep",
	"/api/v1/version",
	// Contrato da frota de status. PUBLICO pelo mesmo motivo que o
	// /api/v1/version logo acima — serve o MESMO metadado de build, que nao e'
	// segredo — e por um motivo proprio: o coletor do painel /status sonda o alvo
	// sem sessao alguma. Atras do Auth, o painel leria 401 como "servico
	// indisponivel", que e' precisamente a mentira que ele existe para evitar.
	// Nao expoe nada alem de {version, built_at}.
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
}

// Outcome is the summary.
type Outcome struct {
	AuthStart, AuthEnd int
	ProtStart, ProtEnd int
	PassCount          int
	Violations         []string
}

// ErrViolations means at least one route is unprotected and un-allowlisted.
var ErrViolations = errors.New("at least one router.go route lacks middleware.Auth protection and lacks an allowlist entry")

// ErrRouterNotFound is emitted when api/internal/server/router.go is missing.
var ErrRouterNotFound = errors.New("router.go not found")

// ErrAuthGroupMissing is emitted when the Auth chi.Router group cannot be
// located — that itself is a systemic regression the gate refuses to gloss over.
var ErrAuthGroupMissing = errors.New("could not locate the middleware.Auth chi.Router group in router.go")

var (
	// routeCallRE matches lines like `\tr.Get("/foo", handler)`. Captures both
	// chi Method / MethodFunc callers.
	routeCallRE = regexp.MustCompile(`^\s*r\.(?:Get|Post|Put|Patch|Delete|Method|MethodFunc)\(`)
	// routePathRE captures the first quoted string in the line, which for
	// chi calls is the route path.
	routePathRE = regexp.MustCompile(`"(/[^"]+)"`)
	// authGroupOpenRE marks the start line of a `r.Group(func(r chi.Router) {`.
	authGroupOpenRE = regexp.MustCompile(`r\.Group\(func\(r chi\.Router\) \{`)
	// protectedFuncOpenRE marks the start line of registerProtectedRoutes body.
	protectedFuncOpenRE = regexp.MustCompile(`^func registerProtectedRoutes\(`)
	// middlewareAuthRE marks any line that references middleware.Auth (used to
	// prove the Group is the Auth group, not some other Group).
	middlewareAuthRE = regexp.MustCompile(`middleware\.Auth`)
)

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
	body, err := os.ReadFile(full)
	if err != nil {
		return Outcome{}, ErrRouterNotFound
	}
	lines := strings.Split(string(body), "\n")

	authStart, authEnd := findAuthGroupRange(lines)
	if authStart == 0 {
		return Outcome{}, ErrAuthGroupMissing
	}
	protStart, protEnd := findProtectedFuncRange(lines)

	pass := 0
	var violations []string
	for i, line := range lines {
		if !routeCallRE.MatchString(line) {
			continue
		}
		m := routePathRE.FindStringSubmatch(line)
		if len(m) < 2 {
			continue
		}
		route := m[1]
		lineNo := i + 1

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
	return outcome, nil
}

// findAuthGroupRange locates the r.Group(func(r chi.Router) { ... }) block
// whose body references middleware.Auth. Ports the bash awk brace-counter.
// Returns 1-based [start, end] line numbers; both zero if not found.
func findAuthGroupRange(lines []string) (int, int) {
	inBlock := false
	startLine := 0
	depth := 0
	hasAuth := false
	for i, line := range lines {
		if !inBlock {
			if authGroupOpenRE.MatchString(line) {
				inBlock = true
				startLine = i + 1
				depth = 1
				hasAuth = false
			}
			continue
		}
		opens := strings.Count(line, "{")
		closes := strings.Count(line, "}")
		depth += opens - closes
		if middlewareAuthRE.MatchString(line) {
			hasAuth = true
		}
		if depth == 0 {
			if hasAuth {
				return startLine, i + 1
			}
			inBlock = false
			startLine = 0
		}
	}
	return 0, 0
}

// findProtectedFuncRange locates the registerProtectedRoutes function body.
// Ports the bash awk brace-counter that tracks first `{` as body opener.
func findProtectedFuncRange(lines []string) (int, int) {
	startLine := 0
	depth := 0
	inBody := false
	for i, line := range lines {
		if startLine == 0 {
			if protectedFuncOpenRE.MatchString(line) {
				startLine = i + 1
				depth = 0
				inBody = false
			}
			// even if the opener has `{` on the same line, fall through to
			// depth accounting below so we don't miss single-line function defs.
		}
		if startLine == 0 {
			continue
		}
		opens := strings.Count(line, "{")
		closes := strings.Count(line, "}")
		if !inBody && opens > 0 {
			inBody = true
		}
		depth += opens - closes
		if inBody && depth == 0 {
			return startLine, i + 1
		}
	}
	return 0, 0
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
