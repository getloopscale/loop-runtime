// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/coveragemerge"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "coverage-merge",
		Summary: "Merges per-package coverage profiles into one report.",
		Wiring:  utility,
		NoRoot:  true,
		Setup:   setupCoverageMerge,
	})
}

// setupCoverageMerge replaces coverage-merge.sh: merges Go + TS lcov into one.
func setupCoverageMerge(fs *flag.FlagSet) Check {
	goProfile := fs.String("go-profile", "", "Go coverage profile path")
	cliLCOV := fs.String("cli-lcov", "", "TypeScript lcov path")
	output := fs.String("output", "", "merged lcov destination (required)")
	return func(inv Invocation) (verdict.Result, error) {
		if *output == "" {
			return nil, usageError("--output required")
		}
		outcome, err := coveragemerge.Merge(coveragemerge.Config{
			GoProfilePath: *goProfile,
			CLILCOVPath:   *cliLCOV,
			OutputPath:    *output,
		})
		if err != nil {
			return nil, err
		}
		for _, m := range outcome.Messages {
			inv.Stdout.Println(m)
		}
		return outcome, nil
	}
}
