// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/canonicalfixturerepo"
)

func init() {
	register(Gate{
		Name:    "canonical-fixture-repo",
		Summary: "Rejects legacy loopzero-repotest fixture in LIVE test/seed scripts; canonical is getloopscale/loopzerokit-example (M-Migrate2 2026-07-07 replaces infra/tests/check-canonical-fixture-repo_test.sh).",
		Setup:   setupCanonicalFixtureRepo,
	})
}

// setupCanonicalFixtureRepo enforces canonical getloopscale/loopzerokit-example fixture.
// Migrated from infra/tests/check-canonical-fixture-repo_test.sh (M-Migrate2 2026-07-07).
func setupCanonicalFixtureRepo(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := canonicalfixturerepo.Check(canonicalfixturerepo.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, canonicalfixturerepo.ErrViolations) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "canonical-fixture-repo: FAIL — %d legacy loopzero-repotest reference(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				fmt.Fprintf(inv.Stderr, "  %s\n", v)
			}
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "canonical-fixture-repo: PASS — no legacy loopzero-repotest (canonical: getloopscale/loopzerokit-example)")
		return Pass, nil
	}
}
