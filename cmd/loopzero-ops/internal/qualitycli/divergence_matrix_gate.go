// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/divergencematrix"
)

func init() {
	register(Gate{
		Name:    "divergence-matrix",
		Summary: "Checks the local-vs-prod divergence matrix is current.",
		Setup:   setupDivergenceMatrix,
	})
}

func setupDivergenceMatrix(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := divergencematrix.Check(divergencematrix.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass {
			fmt.Fprintf(inv.Stderr, "divergence-matrix: FAIL — %d issue(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				fmt.Fprintf(inv.Stderr, "  %s\n", v)
			}
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "divergence-matrix: GREEN — matrix integrity OK")
		return Pass, nil
	}
}
