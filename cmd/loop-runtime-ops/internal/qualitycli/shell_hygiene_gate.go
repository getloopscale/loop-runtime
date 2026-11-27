// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/shellhygiene"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "shell-hygiene",
		Summary: "Lints shell scripts for strict-mode and hygiene rules.",
		Setup:   setupShellHygiene,
	})
}

// setupShellHygiene is the Go home for the shell-hygiene gate (migrated from
// infra/tests/scripts/check-shell-hygiene_test.sh). Every non-lib .sh under
// infra/scripts, infra/helmfile/scripts, infra/tests must set a `set -euo
// pipefail` variant in its top 60 lines.
func setupShellHygiene(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := shellhygiene.Check(shellhygiene.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("shell-hygiene: FAIL — %d / %d scripts missing set -euo pipefail in top 60 lines:\n", len(outcome.Violations), outcome.Total)
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Add 'set -euo pipefail' near the top. Sourced libs in **/lib/*.sh are exempt.")
			return outcome, nil
		}
		inv.Stdout.Printf("shell-hygiene: PASS — %d scripts have set -euo pipefail (libs in **/lib/*.sh exempt)\n", outcome.Total)
		return outcome, nil
	}
}
