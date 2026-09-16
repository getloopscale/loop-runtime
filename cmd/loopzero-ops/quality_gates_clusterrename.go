// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/clusterrenameguard"
)

// runClusterRenameGuard proves that the cluster rename (B-001: mgmt → core,
// vault → secrets) touched only cluster identifiers, never a line where the
// same word meant something else.
//
// T0.1 of .claude/knowledge-base/plans/cluster-rename-core-secrets-plan.md. At
// 1.388 changed occurrences a human reviewing the diff catches errors of form,
// not of classification — so this gate is the real reviewer.
func runClusterRenameGuard(args []string) int {
	fs := flag.NewFlagSet("cluster-rename-guard", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	// Versioned, so it reaches whoever clones. It lived under `.claude/` until
	// 2026-09-16 — gitignored, and deleted with 1,380 other files by `3d4075ca3` — which
	// made this gate fail on a missing file rather than report anything about the rename.
	baseline := fs.String("baseline",
		"infra/quality/snapshots/cluster-topology-naming-generic-baseline.json",
		"baseline of GENERIC occurrences captured before the rename")
	skipAncestry := fs.Bool("skip-ancestry-check", false,
		"skip the guard that refuses a baseline captured after the rename began (EC-1)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	outcome, err := clusterrenameguard.Check(clusterrenameguard.Config{
		RepoRoot:          *root,
		BaselinePath:      *baseline,
		SkipAncestryCheck: *skipAncestry,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "cluster-rename-guard: FAIL — %v\n", err)
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	fmt.Printf("cluster-rename-guard: PASS — %d generic occurrence(s) survived the rename unchanged\n", outcome.Checked)
	return 0
}
