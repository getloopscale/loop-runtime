// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/scheduledimage"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "scheduled-image",
		Summary: "Autonomous execution (scheduled or event-driven) must not use an image of ours that has no publication path in this tree.",
		Setup:   setupScheduledImage,
	})
}

func setupScheduledImage(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := scheduledimage.Check(scheduledimage.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Fail {
			inv.Stderr.Printf("scheduled-image: FAIL — %v\n", out.Violation)
			for _, f := range out.Findings {
				inv.Stderr.Printf("  %s [%s] %s\n", f.File, f.Chart, f.Image)
			}
			return out, nil
		}
		inv.Stdout.Printf("scheduled-image: PASS — %s\n", out.Reason)
		return out, nil
	}
}
