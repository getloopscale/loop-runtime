// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/divergencematrix"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "divergence-matrix",
		Summary: "Checks the local-vs-prod divergence matrix is current.",
		Setup:   setupDivergenceMatrix,
	})
}

func setupDivergenceMatrix(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := divergencematrix.Check(divergencematrix.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass {
			inv.Stderr.Printf("divergence-matrix: FAIL — %d issue(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			return Fail, nil
		}
		inv.Stdout.Println("divergence-matrix: GREEN — matrix integrity OK")
		return Pass, nil
	}
}
