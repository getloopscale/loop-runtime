// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"errors"
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/shellcheckadvisory"
)

func init() {
	register(Gate{
		Name:    "shellcheck-advisory",
		Summary: "Advisory shell-lint gate — runs shellcheck across infra/scripts/local + infra/tests/check-*-syntax_test + infra/tests/e2e/live/dev-public-*. Tolerates SC1091/SC2155/SC2086/SC2046. Advisory-only fallthrough when shellcheck not installed (WARN + exit 0). M-Migrate2 2026-07-07 replaces infra/scripts/local/shellcheck-advisory.sh.",
		Setup:   setupShellcheckAdvisory,
	})
}

// setupShellcheckAdvisory runs shellcheck as an advisory-only quality gate.
// When shellcheck is missing on PATH, emits WARN + exits 0 (advisory).
// When shellcheck is installed, exits 1 on violations (excluding tolerated
// codes: SC1091, SC2155, SC2086, SC2046).
// Migrated from infra/scripts/local/shellcheck-advisory.sh
// (M-Migrate2 continuation 2026-07-07).
func setupShellcheckAdvisory(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := shellcheckadvisory.Check(shellcheckadvisory.Config{RepoRoot: inv.Root})
		if errors.Is(err, shellcheckadvisory.ErrNothingScanned) {
			fmt.Fprintf(inv.Stderr, "shellcheck-advisory: FAIL — %v\n", err)
			return Fail, nil
		}
		if err != nil {
			if !isViolation(err, shellcheckadvisory.ErrViolations) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "shellcheck-advisory: FAIL — %d file(s) scanned; violations follow:\n%s\n", outcome.ScannedFiles, outcome.Diagnostic)
			return Fail, nil
		}
		if outcome.Warn {
			fmt.Fprintln(inv.Stdout, "[shellcheck] WARN: shellcheck not installed — gate is advisory-only.")
			fmt.Fprintln(inv.Stdout, "[shellcheck] Install via apt/brew/go to enable enforcement (R1-F5 T4.1).")
			return Pass, nil
		}
		fmt.Fprintf(inv.Stdout, "shellcheck-advisory: PASS — %d files clean (SC1091/SC2155/SC2086/SC2046 tolerated)\n", outcome.ScannedFiles)
		return Pass, nil
	}
}
