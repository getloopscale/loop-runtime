// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/danglingdata"
)

func init() {
	register(Gate{
		Name:    "dangling-data-refs",
		Summary: "Detects data/file references that no longer resolve after a change.",
		Setup:   setupDanglingDataRefs,
	})
}

// setupDanglingDataRefs is the mirror backstop: fail if a deleted config/data
// file is still referenced (live) by an executable caller. Closes the
// data-orphan class (the 2026-05-11 consolidation deleted maturity-scoring data
// but left the governance scripts dangling). Requires fetch-depth: 0.
func setupDanglingDataRefs(fs *flag.FlagSet) Check {
	baseRef := fs.String("base-ref", "origin/develop", "base git ref")
	headRef := fs.String("head-ref", "HEAD", "head git ref")
	return func(inv Invocation) (Outcome, error) {
		outcome, err := danglingdata.Check(danglingdata.Config{
			RepoRoot: inv.Root,
			BaseRef:  *baseRef,
			HeadRef:  *headRef,
		})
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass {
			fmt.Fprintln(inv.Stderr, "dangling-data-refs: FAIL — deleted config/data files still referenced by live callers:")
			for _, v := range outcome.Violations {
				fmt.Fprintf(inv.Stderr, "  - %s\n", v)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "When deleting a config/data tree, remove or rewire the scripts that read it in")
			fmt.Fprintln(inv.Stderr, "the SAME change. See finding-2026-05-23-governance-subsystem-orphaned.md")
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "dangling-data-refs: PASS — no deleted data file left with live references")
		return Pass, nil
	}
}
