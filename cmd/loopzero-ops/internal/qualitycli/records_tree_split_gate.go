// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/recordstreesplit"
)

func init() {
	register(Gate{
		Name:    "records-tree-split",
		Summary: "One records tree is written; the retired one stays frozen (a second live tree accumulates half the truth).",
		Setup:   setupRecordsTreeSplit,
	})
}

// setupRecordsTreeSplit is the Go home for the records-tree-split gate (B-166).
//
// This repository keeps two records trees on disk and writes only one of them.
// Two written trees never error — they accumulate half the truth, and a reader
// who checks the wrong one reports absence where evidence exists. The gate pins
// the retired tree's `.md` set to a manifest and refuses drift either way.
func setupRecordsTreeSplit(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		o := recordstreesplit.Check(recordstreesplit.Config{RepoRoot: inv.Root})
		if o.LiveMissing {
			fmt.Fprintf(inv.Stderr, "records-tree-split: NOT VERIFIED — %s is absent on this machine.\n"+
				"  It is gitignored, so a fresh clone has none; there is no records tree here to check.\n",
				recordstreesplit.DefaultLiveDir)
			return Fail, fmt.Errorf("%w: %s is absent", errNotVerified, recordstreesplit.DefaultLiveDir)
		}
		if o.Pass && o.ArchiveAbsent {
			fmt.Fprintf(inv.Stdout, "OK one live records tree (%s, %d records); no retired archive at %s, so nothing is frozen\n",
				recordstreesplit.DefaultLiveDir, o.LiveCount, recordstreesplit.DefaultArchiveDir)
			return Pass, nil
		}
		if o.Pass {
			fmt.Fprintf(inv.Stdout, "OK one live records tree (%s, %d records); %s frozen at %d\n",
				recordstreesplit.DefaultLiveDir, o.LiveCount,
				recordstreesplit.DefaultArchiveDir, o.FrozenSize)
			return Pass, nil
		}
		for _, v := range o.Violations {
			fmt.Fprintf(inv.Stderr, "VIOLATION: %s\n", v)
		}
		for _, p := range o.Added {
			fmt.Fprintf(inv.Stderr, "VIOLATION: written to the frozen archive: %s/%s\n",
				recordstreesplit.DefaultArchiveDir, p)
		}
		for _, p := range o.Removed {
			fmt.Fprintf(inv.Stderr, "VIOLATION: deleted from the frozen archive: %s/%s\n",
				recordstreesplit.DefaultArchiveDir, p)
		}
		if len(o.Added) > 0 {
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintf(inv.Stderr, "The records split reopened. Cycle artifacts belong in %s; %s is a\n",
				recordstreesplit.DefaultLiveDir, recordstreesplit.DefaultArchiveDir)
			fmt.Fprintln(inv.Stderr, "read-only archive (see its README.md).")
		}
		if len(o.Removed) > 0 {
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "Nothing in the archive is duplicated in the live tree, so a deleted record")
			fmt.Fprintln(inv.Stderr, "is the only copy of its evidence. Restore it, or re-freeze")
			fmt.Fprintf(inv.Stderr, "%s if the removal is deliberate.\n",
				recordstreesplit.DefaultManifestPath)
		}
		return Fail, nil
	}
}
