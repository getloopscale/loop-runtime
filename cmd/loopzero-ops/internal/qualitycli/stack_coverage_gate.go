// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/stackcoverage"
)

func init() {
	register(Gate{
		Name:    "stack-coverage",
		Summary: "Verifies the tool-inventory stack-coverage claims via grep.",
		Setup:   setupStackCoverage,
	})
}

func setupStackCoverage(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		o, err := stackcoverage.Check(stackcoverage.Config{Root: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !o.Pass {
			if len(o.TypeErrors) > 0 {
				fmt.Fprintln(inv.Stderr, "[stack-coverage] strategy type drift in languages.go:")
				for _, e := range o.TypeErrors {
					fmt.Fprintf(inv.Stderr, "  %s\n", e)
				}
			}
			if len(o.Missing) > 0 {
				fmt.Fprintln(inv.Stderr, "[stack-coverage] strategies without E2E fixture coverage:")
				for _, m := range o.Missing {
					fmt.Fprintf(inv.Stderr, "  %s (add infra/tests/e2e/fixtures/<name>/loopzero.yaml with framework: %s)\n", m, m)
				}
			}
			fmt.Fprintln(inv.Stderr, "[stack-coverage] add fixtures OR deregister the strategy from languages.go::ResolveStrategy.")
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "[stack-coverage] PASS: all %d strategies have fixture coverage.\n", o.StrategyCount)
		return Pass, nil
	}
}
