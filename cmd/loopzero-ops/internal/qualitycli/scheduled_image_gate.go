// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/scheduledimage"
)

func init() {
	register(Gate{
		Name:    "scheduled-image",
		Summary: "Autonomous execution (scheduled or event-driven) must not use an image of ours that has no publication path in this tree.",
		Setup:   setupScheduledImage,
	})
}

func setupScheduledImage(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		out, err := scheduledimage.Check(scheduledimage.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, scheduledimage.ErrNoPublicationPath) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "scheduled-image: FAIL — %v\n", err)
			for _, f := range out.Findings {
				fmt.Fprintf(inv.Stderr, "  %s [%s] %s\n", f.File, f.Chart, f.Image)
			}
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "scheduled-image: PASS — %s\n", out.Reason)
		return Pass, nil
	}
}
