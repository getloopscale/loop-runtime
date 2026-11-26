// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"
	"os"
	"strconv"
	"time"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/spotcheck"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "spotcheck-dimension",
		Summary: "Spot-checks a maturity-audit dimension against evidence.",
		// It picks the sample for the nightly spot check and reports no verdict;
		// the seed comes from --seed, SPOTCHECK_SEED or the clock. Filed as
		// operatorRun until the probe ran it in-process and it concluded
		// cleanly with SPOTCHECK_SEED absent: the seed is optional, not an input
		// the gate cannot run without.
		Wiring: utility,
		NoRoot: true,
		Setup:  setupSpotcheckDimension,
	})
}

// setupSpotcheckDimension replaces spotcheck-dimension.sh: seeded pick of one
// audit dimension + persona for the nightly adversarial spotcheck.
func setupSpotcheckDimension(fs *flag.FlagSet) Check {
	seed := fs.Int64("seed", 0, "selection seed (0 → $SPOTCHECK_SEED or time.Now)")
	return func(inv Invocation) (verdict.Result, error) {
		s := *seed
		if s == 0 {
			if env := os.Getenv("SPOTCHECK_SEED"); env != "" {
				v, err := strconv.ParseInt(env, 10, 64)
				if err != nil {
					return nil, usageError("SPOTCHECK_SEED=%q is not an integer", env)
				}
				s = v
			}
		}
		if s == 0 {
			s = time.Now().Unix()
		}
		outcome, err := spotcheck.Run(spotcheck.Config{Seed: s})
		if err != nil {
			return nil, err
		}
		for _, m := range outcome.Messages {
			inv.Stdout.Println(m)
		}
		return outcome, nil
	}
}
