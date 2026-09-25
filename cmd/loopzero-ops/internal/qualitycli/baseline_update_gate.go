// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/baselineupdate"
)

func init() {
	register(Gate{
		Name:    "baseline-update",
		Summary: "Updates the committed steady-state baseline artifact.",
		Wiring:  utility,
		NoRoot:  true,
		Setup:   setupBaselineUpdate,
	})
}

// setupBaselineUpdate replaces baseline-update.sh: regenerates the coverage
// baseline JSON. --dry-run prints the would-be JSON to stdout.
func setupBaselineUpdate(fs *flag.FlagSet) Check {
	goProfile := fs.String("go-profile", "", "Go coverage profile")
	lcov := fs.String("lcov", "", "CLI lcov.info")
	baseline := fs.String("baseline", "", "baseline JSON write target")
	dryRun := fs.Bool("dry-run", false, "print would-be JSON to stdout instead of writing")
	return func(inv Invocation) (Outcome, error) {
		// Env fallback matches bash contract (DRY_RUN, GO_PROFILE, CLI_LCOV, BASELINE_FILE).
		result, err := baselineupdate.Run(baselineupdate.Config{
			GoProfilePath: firstNonEmpty(*goProfile, os.Getenv("GO_PROFILE")),
			LCOVPath:      firstNonEmpty(*lcov, os.Getenv("CLI_LCOV")),
			BaselinePath:  firstNonEmpty(*baseline, os.Getenv("BASELINE_FILE")),
			DryRun:        *dryRun || os.Getenv("DRY_RUN") == "1",
		})
		if err != nil {
			return Fail, err
		}
		if *dryRun {
			inv.Stdout.Print(string(result.JSON))
		} else {
			inv.Stderr.Printf("baseline-update: wrote %d packages to %s\n", result.PackagesWritten, result.OutputPath)
		}
		return Pass, nil
	}
}
