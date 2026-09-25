// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/sprintsyntax"
)

func init() {
	register(Gate{
		Name:    "sprint-syntax",
		Summary: "Hermetic contract test for infra/scripts/local/check-sprint.sh + its homed helper check_sprint.py: script exists + bash -n clean + helper compiles (python3 -m py_compile) + no python3 heredoc (T3.2 de-costume gate) + JSON report shape (pass count + spaced-detail preservation + [id,status,detail] tuple layout) + allowlist entry present. Requires bash + python3 on PATH. M-Migrate2 2026-07-10 M16 T2.1 Wave 1 replaces infra/tests/check-sprint-syntax_test.sh.",
		Setup:   setupSprintSyntax,
	})
}

// setupSprintSyntax enforces the check-sprint.sh hermetic contract: script exists,
// bash -n clean, helper compiles, no python3 heredoc, JSON report shape valid,
// allowlist entry present. Requires bash + python3 on PATH.
// Migrated from infra/tests/check-sprint-syntax_test.sh
// (M-Migrate2 2026-07-10 M16 T2.1 Wave 1).
func setupSprintSyntax(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := sprintsyntax.Check(sprintsyntax.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, sprintsyntax.ErrViolations) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "sprint-syntax: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				fmt.Fprintf(inv.Stderr, "  %s\n", f)
			}
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "sprint-syntax: PASS — 11 hermetic contract assertions clean (script + bash-n + helper + no-heredoc + json-shape + allowlist)")
		return Pass, nil
	}
}
