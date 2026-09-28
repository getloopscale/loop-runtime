// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/pkgusagematrix"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "pkg-usage-matrix",
		Summary: "Verifies the pkg/ cross-component usage matrix is current.",
		Setup:   setupPkgUsageMatrix,
	})
}

// setupPkgUsageMatrix is the gate replacing pkg-usage-matrix.sh: keeps the pkg
// Real Usage Matrix doc in sync with actual imports. --check verifies, --write
// regenerates. Pure-logic (grep of imports), hermetic.
func setupPkgUsageMatrix(fs *flag.FlagSet) Check {
	docPath := fs.String("doc-path", "", "override matrix doc path")
	write := fs.Bool("write", false, "regenerate the matrix (default: --check only)")
	fs.Bool("check", true, "verify matrix freshness (default)")
	return func(inv Invocation) (verdict.Result, error) {
		mode := pkgusagematrix.ModeCheck
		if *write {
			mode = pkgusagematrix.ModeWrite
		}

		outcome, err := pkgusagematrix.Run(pkgusagematrix.Config{
			Root:    inv.Root,
			DocPath: *docPath,
		}, mode)
		if err != nil {
			return nil, err
		}
		for _, m := range outcome.Messages {
			inv.Stdout.Println(m)
		}
		if outcome.Verdict() == verdict.Fail {
			return outcome, nil
		}
		return outcome, nil
	}
}
