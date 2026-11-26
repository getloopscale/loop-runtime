// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"os"
	"path/filepath"
	"time"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/testpyramid"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "test-pyramid",
		Summary: "Validates the unit/integration/e2e test-pyramid balance.",
		Setup:   setupTestPyramid,
	})
}

func setupTestPyramid(fs *flag.FlagSet) Check {
	base := fs.String("baseline", "infra/quality/test-pyramid-baseline.json", "frozen measured pyramid")
	write := fs.Bool("write", false, "re-freeze the baseline from the current tree")
	return func(inv Invocation) (verdict.Result, error) {
		basePath := filepath.Join(inv.Root, *base)

		// The PR label remains the way out for a single PR. It comes first because it
		// is the one case where NOT measuring is the right answer.
		if d, ok := testpyramid.Deferral(os.Getenv("PR_LABELS")); ok {
			inv.Stdout.Println("[pyramid] label 'pyramid-defer' — PASS (open the automatic issue)")
			return d, nil
		}

		if *write {
			frozen, err := testpyramid.Refreeze(inv.Root, basePath, time.Now().UTC().Format("2006-01-02"))
			if err != nil {
				return nil, err
			}
			inv.Stdout.Printf("[pyramid] baseline frozen: %d files — unit %.1f%% / integration %.1f%% / e2e %.1f%%\n",
				frozen.Total, frozen.UnitPct, frozen.IntegPct, frozen.E2EPct)
			return frozen, nil
		}

		o, err := testpyramid.CheckDerived(inv.Root, basePath, testpyramid.DefaultThresholds())
		if err != nil {
			return nil, err
		}
		if o.Verdict() == verdict.Fail {
			inv.Stderr.Printf("[pyramid] FAIL — %v\n", o.Violation)
			for _, v := range o.Regressions {
				inv.Stderr.Printf("  %s\n", v)
			}
			inv.Stderr.Println("[pyramid] the ratchet only allows improvement; use the 'pyramid-defer' label for a single PR")
			return o, nil
		}
		inv.Stdout.Printf("[pyramid] PASS — %d files derived from the tree: unit %.1f%% / integration %.1f%% / e2e %.1f%%\n",
			o.Total, o.UnitPct, o.IntegPct, o.E2EPct)
		// The distance to the ADR target is reported and does NOT block: that target was
		// calibrated against a file that declared 0.1% e2e. Silencing it would hide the
		// pending decision; blocking on it would create a lock the repo cannot satisfy.
		for _, g := range o.AspirationGaps {
			inv.Stdout.Printf("[pyramid] distance to the ADR D7 target (reported, does not block): %s\n", g)
		}
		return o, nil
	}
}
