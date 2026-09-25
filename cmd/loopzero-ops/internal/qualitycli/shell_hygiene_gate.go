// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/shellhygiene"
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
	return func(inv Invocation) (Outcome, error) {
		outcome, err := shellhygiene.Check(shellhygiene.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass {
			fmt.Fprintf(inv.Stderr, "shell-hygiene: FAIL — %d / %d scripts missing set -euo pipefail in top 60 lines:\n", len(outcome.Violations), outcome.Total)
			for _, v := range outcome.Violations {
				fmt.Fprintf(inv.Stderr, "  %s\n", v)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "Add 'set -euo pipefail' near the top. Sourced libs in **/lib/*.sh are exempt.")
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "shell-hygiene: PASS — %d scripts have set -euo pipefail (libs in **/lib/*.sh exempt)\n", outcome.Total)
		return Pass, nil
	}
}
