// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/advisorylock"
)

func init() {
	register(Gate{
		Name:    "advisory-lock-scope",
		Summary: "Forbids session-scoped pg_advisory_lock; only pg_advisory_xact_lock is allowed.",
		Setup:   setupAdvisoryLockScope,
	})
}

// setupAdvisoryLockScope is the Go home for the advisory-lock-scope gate (migrated
// from infra/tests/scripts/check-advisory-lock-scope_test.sh). Forbids
// session-scoped pg_advisory_lock( in production code (CLAUDE.md Rule 7).
func setupAdvisoryLockScope(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := advisorylock.Check(advisorylock.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass() {
			inv.Stderr.Printf("advisory-lock-scope: FAIL — %d session-scoped pg_advisory_lock( call(s):\n", len(outcome.Findings))
			for _, v := range outcome.Findings {
				inv.Stderr.Printf("  %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Fix: replace with pg_advisory_xact_lock( — transaction-scoped, pool-safe (Rule 7).")
			return Fail, nil
		}
		inv.Stdout.Printf("advisory-lock-scope: PASS — no session-scoped pg_advisory_lock( in production code (%s)\n", outcome.Summary())
		return Pass, nil
	}
}
