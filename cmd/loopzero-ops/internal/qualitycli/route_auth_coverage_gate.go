// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"errors"
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/routeauthcoverage"
)

func init() {
	register(Gate{
		Name:    "route-auth-coverage",
		Summary: "Enforces R2-F8 (HIGH) — every api/internal/server/router.go route MUST be inside middleware.Auth group OR registerProtectedRoutes OR explicit public-allowlist. Locates group ranges via brace-counter, enumerates r.{Get,Post,Put,Patch,Delete,Method}( calls. M-Migrate2 2026-07-07 replaces infra/tests/check-route-auth-coverage_test.sh.",
		Setup:   setupRouteAuthCoverage,
	})
}

// setupRouteAuthCoverage enforces R2-F8 (HIGH): every user-facing router.go route
// MUST be inside middleware.Auth group OR registerProtectedRoutes OR appear in
// the explicit public-allowlist.
// Migrated from infra/tests/check-route-auth-coverage_test.sh
// (M-Migrate2 continuation 2026-07-07).
func setupRouteAuthCoverage(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := routeauthcoverage.Check(routeauthcoverage.Config{RepoRoot: inv.Root})
		if errors.Is(err, routeauthcoverage.ErrNoRoutesEnumerated) {
			// Named apart from the generic failure so the operator reads the CAUSE.
			// "0 routes" is the one failure whose fix is never in router.go: either
			// the registrations moved (B-041) and the walker must follow them, or
			// the receiver was renamed and the regexp in check.go:84 no longer
			// matches. Printing only the error would send the reader hunting for an
			// unprotected route that does not exist.
			fmt.Fprintf(inv.Stderr, "route-auth-coverage: FAIL — %v\n", err)
			fmt.Fprintf(inv.Stderr, "  Auth group spans lines %d..%d, registerProtectedRoutes %d..%d — both located.\n",
				outcome.AuthStart, outcome.AuthEnd, outcome.ProtStart, outcome.ProtEnd)
			fmt.Fprintln(inv.Stderr, "  Fix: point the walker at wherever the r.{Get,Post,...} calls now live, or restore them to router.go.")
			return Fail, err
		}
		if err != nil {
			if !isViolation(err, routeauthcoverage.ErrViolations, routeauthcoverage.ErrUndeclaredOffRouterMount) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "route-auth-coverage: FAIL — %v\n", err)
			if len(outcome.Violations) > 0 {
				fmt.Fprintf(inv.Stderr, "  Auth group spans lines %d..%d\n", outcome.AuthStart, outcome.AuthEnd)
				fmt.Fprintf(inv.Stderr, "  registerProtectedRoutes spans lines %d..%d\n", outcome.ProtStart, outcome.ProtEnd)
				for _, v := range outcome.Violations {
					fmt.Fprintf(inv.Stderr, "  %s\n", v)
				}
				fmt.Fprintln(inv.Stderr, "Fix: move the route inside middleware.Auth group OR add explicit allowlist entry with rationale.")
			}
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "route-auth-coverage: PASS — %d routes either in Auth group or allowlisted (auth spans %d..%d, protected %d..%d)\n",
			outcome.PassCount, outcome.AuthStart, outcome.AuthEnd, outcome.ProtStart, outcome.ProtEnd)
		return Pass, nil
	}
}
