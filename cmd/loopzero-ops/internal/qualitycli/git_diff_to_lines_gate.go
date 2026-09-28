// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"context"
	"flag"
	"os"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/gitdiff"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/shellrun"
)

func init() {
	register(Gate{
		Name:    "git-diff-to-lines",
		Summary: "Utility: converts a git diff into a changed-line set for other gates.",
		Wiring:  utility,
		NoRoot:  true,
		Setup:   setupGitDiffToLines,
	})
}

// setupGitDiffToLines replaces git-diff-to-lines.sh (a bash+Python wrapper):
// runs `git diff --unified=0 base...head` and emits one `path:line` per
// touched line. Parse logic lives in the tested cmd/loopzero-ops/internal/quality/gitdiff.
func setupGitDiffToLines(fs *flag.FlagSet) Check {
	base := fs.String("base", "", "base ref (falls back to $BASE_REF, then origin/main)")
	head := fs.String("head", "", "head ref (falls back to $HEAD_REF, then HEAD)")
	return func(inv Invocation) (verdict.Result, error) {
		b := firstNonEmpty(*base, os.Getenv("BASE_REF"), "origin/main")
		h := firstNonEmpty(*head, os.Getenv("HEAD_REF"), "HEAD")
		pairs, err := gitdiff.RunGitDiff(context.Background(), shellrun.NewDefault(), gitdiff.Config{BaseRef: b, HeadRef: h})
		if err != nil {
			return nil, err
		}
		if s := gitdiff.FormatLines(pairs); s != "" {
			inv.Stdout.Print(s)
			if !strings.HasSuffix(s, "\n") {
				inv.Stdout.Println()
			}
		}
		return pairs, nil
	}
}
