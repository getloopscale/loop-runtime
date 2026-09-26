// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/allowlistsunsets"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "check-allowlist-sunsets",
		Summary: "Every exemption in rules/code-review-allowlist.txt carries a sunset, and on that date it stops exempting anything",
		Setup:   setupCheckAllowlistSunsets,
	})
}

// setupCheckAllowlistSunsets is a hermetic contract test for the Python
// .claude/scripts/check-allowlist-sunsets.py gate (T5.2.a fixture verifier).
// Migrated from infra/tests/check-allowlist-sunsets_test.sh
// (M-Migrate2 continuation 2026-07-07).
func setupCheckAllowlistSunsets(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := allowlistsunsets.Check(allowlistsunsets.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, allowlistsunsets.ErrViolations) {
				return Fail, err
			}
			inv.Stderr.Printf("check-allowlist-sunsets: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			return Fail, nil
		}
		// Report what was actually read. This line claimed "7 fixture cases" until 2026-09-16,
		// when the fixtures went with the missing Python script they graded — a success
		// message describing work that no longer happens is the same defect as a gate whose
		// remedy names an absent path.
		for _, w := range outcome.Warnings {
			inv.Stdout.Printf("check-allowlist-sunsets: WARN — %s\n", w)
		}
		inv.Stdout.Printf("check-allowlist-sunsets: PASS — %d exemption(s) checked, none expired\n", outcome.Checked)
		return Pass, nil
	}
}
