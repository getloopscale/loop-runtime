// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/decisionsdeletion"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
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
	return func(inv Invocation) (verdict.Result, error) {
		o, err := decisionsdeletion.Check(decisionsdeletion.Config{RepoRoot: inv.Root, BaseRef: *base, HeadRef: *head})
		if err != nil {
			return Fail, err
		}
		for _, s := range o.Superseded {
			inv.Stdout.Printf("✓ %s (properly superseded)\n", s)
		}
		if !o.Pass {
			inv.Stderr.Printf("✗ VIOLATION: %d protected file(s) deleted without a superseding record:\n", len(o.Violations))
			for _, v := range o.Violations {
				inv.Stderr.Printf("  - %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Deleting a protected governance document (docs/decisions.md, docs/runbooks.md)")
			inv.Stderr.Println("requires a sibling .md with 'superseded_by: <deleted-basename>' frontmatter.")
			return Fail, nil
		}
		if len(o.DeletedFiles) == 0 {
			inv.Stdout.Println("✓ No protected governance document deleted. Stability hook clean.")
		} else {
			inv.Stdout.Printf("✓ All %d deletion(s) properly superseded. Stability hook clean.\n", len(o.DeletedFiles))
		}
		return Pass, nil
	}
}
