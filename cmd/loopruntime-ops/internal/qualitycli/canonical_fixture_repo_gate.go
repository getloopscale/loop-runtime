// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/canonicalfixturerepo"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "canonical-fixture-repo",
		Summary: "Rejects legacy loopruntime-repotest fixture in LIVE test/seed scripts; canonical is getloopscale/loopzerokit-example (M-Migrate2 2026-07-07 replaces infra/tests/check-canonical-fixture-repo_test.sh).",
		Setup:   setupCanonicalFixtureRepo,
	})
}

// setupCanonicalFixtureRepo enforces canonical getloopscale/loopzerokit-example fixture.
// Migrated from infra/tests/check-canonical-fixture-repo_test.sh (M-Migrate2 2026-07-07).
func setupCanonicalFixtureRepo(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := canonicalfixturerepo.Check(canonicalfixturerepo.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("canonical-fixture-repo: FAIL — %d legacy loopruntime-repotest reference(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			return outcome, nil
		}
		inv.Stdout.Println("canonical-fixture-repo: PASS — no legacy loopruntime-repotest (canonical: getloopscale/loopzerokit-example)")
		return outcome, nil
	}
}
