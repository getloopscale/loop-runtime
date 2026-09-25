// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/correctionstxt"
)

func init() {
	register(Gate{
		Name:    "corrections-txt-closed",
		Summary: "Asserts CORRECTIONS.txt items are all closed.",
		Setup:   setupCorrectionsTxtClosed,
	})
}

// setupCorrectionsTxtClosed is the Go home for the corrections-txt-closed gate
// (migrated from infra/tests/check-corrections-txt-closed_test.sh). Enforces
// close-out hygiene on the drill war-journal infra/tests/performance/load/CORRECTIONS.txt.
func setupCorrectionsTxtClosed(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := correctionstxt.Check(filepath.Join(inv.Root, "infra", "tests", "performance", "load", "CORRECTIONS.txt"))
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass {
			inv.Stderr.Printf("corrections-txt-closed: FAIL — %d close-out violation(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			return Fail, nil
		}
		inv.Stdout.Println("corrections-txt-closed: PASS — all CORRECTIONS.txt items are annotated + closed out")
		return Pass, nil
	}
}
