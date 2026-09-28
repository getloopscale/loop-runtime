// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/readinesssummary"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "readiness-summary",
		Summary: "The readiness summary is the last status mutation before persisting, in all six reconcilers.",
		Setup:   setupReadinessSummary,
	})
}

func setupReadinessSummary(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := readinesssummary.Check(readinesssummary.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Fail {
			inv.Stderr.Printf("readiness-summary: FAIL — %s\n", out.Summary())
			for _, v := range out.Violations {
				inv.Stderr.Printf("  %s:%d — %s() persists .status (%s) without folding the readiness summary, "+
					"neither inline nor by a defer in its caller. A pass through here leaves the Ready condition "+
					"with what the PREVIOUS pass computed, and that is read as current (ADR-0025 D3, B-105).\n",
					v.File, v.Line, v.Func, v.Shape)
			}
			return out, nil
		}
		inv.Stdout.Printf("readiness-summary: PASS — %s\n", out.Summary())
		return out, nil
	}
}
