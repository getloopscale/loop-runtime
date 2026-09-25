// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/clusterrenameguard"
)

func init() {
	register(Gate{
		Name:    "cluster-rename-guard",
		Summary: "Proves the cluster rename (B-001: mgmt->core, vault->secrets) touched only cluster identifiers. Asserts every line a pre-rename baseline classified GENERIC still exists byte-for-byte — `vault` carries 74 such lines naming OpenBao, `mgmt` carries zero. Refuses to run when the baseline cannot be proven an ancestor of HEAD, because a baseline captured after the first edit compares the new state with itself and passes forever. T0.1 of plans/cluster-rename-core-secrets-plan.md.",
		Setup:   setupClusterRenameGuard,
	})
}

// setupClusterRenameGuard proves that the cluster rename (B-001: mgmt → core,
// vault → secrets) touched only cluster identifiers, never a line where the
// same word meant something else.
//
// T0.1 of .claude/knowledge-base/plans/cluster-rename-core-secrets-plan.md. At
// 1.388 changed occurrences a human reviewing the diff catches errors of form,
// not of classification — so this gate is the real reviewer.
func setupClusterRenameGuard(fs *flag.FlagSet) Check {
	// Versioned, so it reaches whoever clones. It lived under `.claude/` until
	// 2026-09-16 — gitignored, and deleted with 1,380 other files by `3d4075ca3` — which
	// made this gate fail on a missing file rather than report anything about the rename.
	baseline := fs.String("baseline",
		"infra/quality/snapshots/cluster-topology-naming-generic-baseline.json",
		"baseline of GENERIC occurrences captured before the rename")
	skipAncestry := fs.Bool("skip-ancestry-check", false,
		"skip the guard that refuses a baseline captured after the rename began (EC-1)")
	return func(inv Invocation) (Outcome, error) {
		outcome, err := clusterrenameguard.Check(clusterrenameguard.Config{
			RepoRoot:          inv.Root,
			BaselinePath:      *baseline,
			SkipAncestryCheck: *skipAncestry,
		})
		if err != nil {
			if len(outcome.Failures) == 0 {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "cluster-rename-guard: FAIL — %v\n", err)
			for _, f := range outcome.Failures {
				fmt.Fprintf(inv.Stderr, "  %s\n", f)
			}
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "cluster-rename-guard: PASS — %d generic occurrence(s) survived the rename unchanged\n", outcome.Checked)
		return Pass, nil
	}
}
