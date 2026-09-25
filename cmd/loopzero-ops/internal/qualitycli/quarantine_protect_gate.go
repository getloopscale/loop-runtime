// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/quarantineprotect"
)

func init() {
	register(Gate{
		Name:    "quarantine-protect",
		Summary: "Protects the canonical-tests-v1 quarantine from accidental edits.",
		Setup:   setupQuarantineProtect,
	})
}

// setupQuarantineProtect is the T0.2 governance gate of plan canonical-tests-rewrite-v2 v1.0:
// fail if a PR touches archive/canonical-tests-v1/** during the strangler-fig
// window. DELETE-only allowed with [ARCHIVE-DELETE] PR title marker per ADR D5.
func setupQuarantineProtect(fs *flag.FlagSet) Check {
	archive := fs.String("archive", "archive/canonical-tests-v1", "quarantine path root prefix")
	baseRef := fs.String("base-ref", "origin/main", "base git ref")
	headRef := fs.String("head-ref", "HEAD", "head git ref")
	prTitle := fs.String("pr-title", os.Getenv("GITHUB_PR_TITLE"), "PR title (substring-matched for [ARCHIVE-DELETE] marker)")
	return func(inv Invocation) (Outcome, error) {
		outcome, err := quarantineprotect.Check(quarantineprotect.Config{
			RepoRoot:        inv.Root,
			ArchivePathRoot: *archive,
			BaseRef:         *baseRef,
			HeadRef:         *headRef,
			PRTitle:         *prTitle,
		})
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass {
			fmt.Fprintln(inv.Stderr, "quarantine-protect: FAIL — PR touches quarantined files:")
			for _, v := range outcome.Violations {
				fmt.Fprintf(inv.Stderr, "  - %s\n", v)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "Per ADR-2026-06-17-T3 D5: build the new suite at infra/tests/livetest/ paths instead.")
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "quarantine-protect: PASS — no quarantine violations")
		return Pass, nil
	}
}
