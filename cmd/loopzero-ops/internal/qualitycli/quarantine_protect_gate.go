// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/quarantineprotect"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
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
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := quarantineprotect.Check(quarantineprotect.Config{
			RepoRoot:        inv.Root,
			ArchivePathRoot: *archive,
			BaseRef:         *baseRef,
			HeadRef:         *headRef,
			PRTitle:         *prTitle,
		})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Println("quarantine-protect: FAIL — PR touches quarantined files:")
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  - %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Per ADR-2026-06-17-T3 D5: build the new suite at infra/tests/livetest/ paths instead.")
			return outcome, nil
		}
		inv.Stdout.Println("quarantine-protect: PASS — no quarantine violations")
		return outcome, nil
	}
}
