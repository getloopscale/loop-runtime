// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/decisionsdeletion"
)

func init() {
	register(Gate{
		Name:    "decisions-deletion",
		Summary: "Guards deletion of decision/ADR records.",
		Setup:   setupDecisionsDeletion,
	})
}

func setupDecisionsDeletion(fs *flag.FlagSet) Check {
	base := fs.String("base-ref", "origin/main", "base git ref")
	head := fs.String("head-ref", "HEAD", "head git ref")
	return func(inv Invocation) (Outcome, error) {
		o, err := decisionsdeletion.Check(decisionsdeletion.Config{RepoRoot: inv.Root, BaseRef: *base, HeadRef: *head})
		if err != nil {
			return Fail, err
		}
		for _, s := range o.Superseded {
			fmt.Fprintf(inv.Stdout, "✓ %s (properly superseded)\n", s)
		}
		if !o.Pass {
			fmt.Fprintf(inv.Stderr, "✗ VIOLATION: %d protected file(s) deleted without a superseding record:\n", len(o.Violations))
			for _, v := range o.Violations {
				fmt.Fprintf(inv.Stderr, "  - %s\n", v)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "Deleting a protected governance document (docs/decisions.md, docs/runbooks.md)")
			fmt.Fprintln(inv.Stderr, "requires a sibling .md with 'superseded_by: <deleted-basename>' frontmatter.")
			return Fail, nil
		}
		if len(o.DeletedFiles) == 0 {
			fmt.Fprintln(inv.Stdout, "✓ No protected governance document deleted. Stability hook clean.")
		} else {
			fmt.Fprintf(inv.Stdout, "✓ All %d deletion(s) properly superseded. Stability hook clean.\n", len(o.DeletedFiles))
		}
		return Pass, nil
	}
}
