// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"errors"
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/shellcheckadvisory"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
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
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := shellcheckadvisory.Check(shellcheckadvisory.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		switch {
		case errors.Is(outcome.Violation, shellcheckadvisory.ErrNothingScanned):
			inv.Stderr.Printf("shellcheck-advisory: FAIL — %v\n", outcome.Violation)
		case outcome.Verdict() == verdict.Fail:
			inv.Stderr.Printf("shellcheck-advisory: FAIL — %d file(s) scanned; violations follow:\n%s\n", outcome.ScannedFiles, outcome.Diagnostic)
		case outcome.Verdict() == verdict.Skip:
			inv.Stdout.Println("[shellcheck] WARN: shellcheck not installed — gate is advisory-only.")
			inv.Stdout.Println("[shellcheck] Install via apt/brew/go to enable enforcement (R1-F5 T4.1).")
		default:
			inv.Stdout.Printf("shellcheck-advisory: PASS — %d files clean (SC1091/SC2155/SC2086/SC2046 tolerated)\n", outcome.ScannedFiles)
		}
		return outcome, nil
	}
}
