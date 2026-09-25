// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testinventory"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testpyramid"
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
	return func(inv Invocation) (Outcome, error) {
		basePath := filepath.Join(inv.Root, *base)

		// The PR label remains the way out for a single PR. It comes first because it
		// is the one case where NOT measuring is the right answer.
		for _, lbl := range strings.Split(os.Getenv("PR_LABELS"), ",") {
			if strings.TrimSpace(lbl) == "pyramid-defer" {
				fmt.Fprintln(inv.Stdout, "[pyramid] label 'pyramid-defer' — PASS (open the automatic issue)")
				return Pass, nil
			}
		}

		if *write {
			derived, err := testinventory.Derive(inv.Root)
			if err != nil {
				return Fail, err
			}
			note := "Derived from the tree by testinventory.Derive, not declared (B-106)."
			if err := testpyramid.WriteBaseline(basePath, derived, time.Now().UTC().Format("2006-01-02"), note); err != nil {
				return Fail, err
			}
			u, i, e := derived.Ratios()
			fmt.Fprintf(inv.Stdout, "[pyramid] baseline frozen: %d files — unit %.1f%% / integration %.1f%% / e2e %.1f%%\n",
				derived.Total(), u, i, e)
			return Pass, nil
		}

		o, err := testpyramid.CheckDerived(inv.Root, basePath, testpyramid.DefaultThresholds())
		if err != nil {
			if !isViolation(err, testpyramid.ErrRegressed) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "[pyramid] FAIL — %v\n", err)
			for _, v := range o.Regressions {
				fmt.Fprintf(inv.Stderr, "  %s\n", v)
			}
			fmt.Fprintln(inv.Stderr, "[pyramid] the ratchet only allows improvement; use the 'pyramid-defer' label for a single PR")
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "[pyramid] PASS — %d files derived from the tree: unit %.1f%% / integration %.1f%% / e2e %.1f%%\n",
			o.Total, o.UnitPct, o.IntegPct, o.E2EPct)
		// The distance to the ADR target is reported and does NOT block: that target was
		// calibrated against a file that declared 0.1% e2e. Silencing it would hide the
		// pending decision; blocking on it would create a lock the repo cannot satisfy.
		for _, g := range o.AspirationGaps {
			fmt.Fprintf(inv.Stdout, "[pyramid] distance to the ADR D7 target (reported, does not block): %s\n", g)
		}
		return Pass, nil
	}
}
