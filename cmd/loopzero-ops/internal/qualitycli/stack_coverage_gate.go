// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/stackcoverage"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "stack-coverage",
		Summary: "Verifies the tool-inventory stack-coverage claims via grep.",
		Setup:   setupStackCoverage,
	})
}

func setupStackCoverage(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		o, err := stackcoverage.Check(stackcoverage.Config{Root: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !o.Pass {
			if len(o.TypeErrors) > 0 {
				inv.Stderr.Println("[stack-coverage] strategy type drift in languages.go:")
				for _, e := range o.TypeErrors {
					inv.Stderr.Printf("  %s\n", e)
				}
			}
			if len(o.Missing) > 0 {
				inv.Stderr.Println("[stack-coverage] strategies without E2E fixture coverage:")
				for _, m := range o.Missing {
					inv.Stderr.Printf("  %s (add infra/tests/e2e/fixtures/<name>/loopzero.yaml with framework: %s)\n", m, m)
				}
			}
			inv.Stderr.Println("[stack-coverage] add fixtures OR deregister the strategy from languages.go::ResolveStrategy.")
			return Fail, nil
		}
		inv.Stdout.Printf("[stack-coverage] PASS: all %d strategies have fixture coverage.\n", o.StrategyCount)
		return Pass, nil
	}
}
