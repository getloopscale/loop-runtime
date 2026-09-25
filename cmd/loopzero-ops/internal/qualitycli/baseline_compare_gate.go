// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/baselinecompare"
)

func init() {
	register(Gate{
		Name:    "baseline-compare",
		Summary: "Compares a captured steady-state baseline against the current state.",
		Wiring:  utility,
		NoRoot:  true,
		Setup:   setupBaselineCompare,
	})
}

// setupBaselineCompare replaces baseline-compare.sh: per-package coverage
// regression gate vs the checked-in baseline.
func setupBaselineCompare(fs *flag.FlagSet) Check {
	baseline := fs.String("baseline", "", "checked-in baseline JSON")
	current := fs.String("current", "", "PR-side coverage JSON")
	defaultMin := fs.Float64("default-min", 80.0, "min for newly-added packages")
	slack := fs.Float64("slack", 3.0, "tolerance below baseline (pp)")
	deferLabel := fs.Bool("defer", false, "defer label present (EC-2 escape valve)")
	deferReason := fs.String("defer-reason", "", "defer justification")
	return func(inv Invocation) (Outcome, error) {
		// Env fallback matches the bash env contract (BASELINE_FILE, CURRENT_FILE,
		// DEFER_*). Untrusted PR-derived DEFER_REASON arrives via env, not CLI arg.
		baselinePath := firstNonEmpty(*baseline, os.Getenv("BASELINE_FILE"))
		currentPath := firstNonEmpty(*current, os.Getenv("CURRENT_FILE"))
		deferPresent := *deferLabel || os.Getenv("DEFER_LABEL_PRESENT") == "1"
		reason := firstNonEmpty(*deferReason, os.Getenv("DEFER_REASON"))
		outcome, err := baselinecompare.Run(baselinecompare.Config{
			BaselinePath:      baselinePath,
			CurrentPath:       currentPath,
			DefaultMin:        *defaultMin,
			SlackPP:           *slack,
			DeferLabelPresent: deferPresent,
			DeferReason:       reason,
		})
		if err != nil {
			return Fail, err
		}
		for _, m := range outcome.Messages {
			fmt.Fprintln(inv.Stdout, m)
		}
		if !outcome.Pass {
			return Fail, nil
		}
		return Pass, nil
	}
}
