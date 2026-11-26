// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/coveragediff"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "coverage-diff",
		Summary: "Diffs test coverage against a baseline to catch regressions.",
		Wiring:  utility,
		NoRoot:  true,
		Setup:   setupCoverageDiff,
	})
}

// setupCoverageDiff replaces coverage-diff.sh: diff-aware coverage threshold gate.
func setupCoverageDiff(fs *flag.FlagSet) Check {
	diff := fs.String("diff", "", "touched-lines file (path:line per line)")
	lcov := fs.String("lcov", "", "merged lcov report")
	threshold := fs.Float64("threshold", 80.0, "minimum coverage percent on touched lines")
	report := fs.String("report", "", "optional Markdown report destination")
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := coveragediff.Run(coveragediff.Config{
			DiffPath:   *diff,
			LCOVPath:   *lcov,
			Threshold:  *threshold,
			ReportPath: *report,
		})
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
