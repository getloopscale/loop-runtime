// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"errors"
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/routeauthcoverage"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
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
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := routeauthcoverage.Check(routeauthcoverage.Config{RepoRoot: inv.Root})
		if errors.Is(err, routeauthcoverage.ErrNoRoutesEnumerated) {
			// The gate examined nothing: the runner reports the cause and exits
			// Incomplete. What only this gate can add is where to look. "0 routes"
			// is the one failure whose fix is never in router.go: either the
			// registrations moved (B-041) and the walker must follow them, or the
			// receiver was renamed and the regexp in check.go:84 no longer matches.
			// Printing only the error would send the reader hunting for an
			// unprotected route that does not exist.
			inv.Stderr.Printf("  Auth group spans lines %d..%d, registerProtectedRoutes %d..%d — both located.\n",
				outcome.AuthStart, outcome.AuthEnd, outcome.ProtStart, outcome.ProtEnd)
			inv.Stderr.Println("  Fix: point the walker at wherever the r.{Get,Post,...} calls now live, or restore them to router.go.")
			return nil, err
		}
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("route-auth-coverage: FAIL — %v\n", outcome.Violation)
			if len(outcome.Violations) > 0 {
				inv.Stderr.Printf("  Auth group spans lines %d..%d\n", outcome.AuthStart, outcome.AuthEnd)
				inv.Stderr.Printf("  registerProtectedRoutes spans lines %d..%d\n", outcome.ProtStart, outcome.ProtEnd)
				for _, v := range outcome.Violations {
					inv.Stderr.Printf("  %s\n", v)
				}
				inv.Stderr.Println("Fix: move the route inside middleware.Auth group OR add explicit allowlist entry with rationale.")
			}
			return outcome, nil
		}
		inv.Stdout.Printf("route-auth-coverage: PASS — %d routes either in Auth group or allowlisted (auth spans %d..%d, protected %d..%d)\n",
			outcome.PassCount, outcome.AuthStart, outcome.AuthEnd, outcome.ProtStart, outcome.ProtEnd)
		return outcome, nil
	}
}
