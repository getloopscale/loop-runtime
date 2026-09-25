// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/installgithooks"
)

func init() {
	register(Gate{
		Name:    "install-git-hooks",
		Summary: "Validates the M8 1.5 pre-push hook + installer canonical shape + idempotency (M-Migrate2 2026-07-07 replaces infra/tests/check-install-git-hooks_test.sh).",
		Setup:   setupInstallGitHooks,
	})
}

// setupInstallGitHooks validates the M8 1.5 pre-push hook + installer.
// Migrated from infra/tests/check-install-git-hooks_test.sh (M-Migrate2 2026-07-07).
func setupInstallGitHooks(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := installgithooks.Check(installgithooks.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, installgithooks.ErrFailures) {
				return Fail, err
			}
			inv.Stderr.Printf("install-git-hooks: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			return Fail, nil
		}
		inv.Stdout.Println("install-git-hooks: PASS — pre-push hook + installer canonical shape validated")
		return Pass, nil
	}
}
