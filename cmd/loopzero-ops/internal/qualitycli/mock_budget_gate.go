// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/mockbudget"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "mock-budget",
		Summary: "Caps the number of mocks per test (SRP smell budget).",
		NoRoot:  true,
		Setup:   setupMockBudget,
	})
}

func setupMockBudget(fs *flag.FlagSet) Check {
	dir := fs.String("dir", ".", "target directory to scan")
	budget := fs.Int("budget", mockbudget.DefaultBudget, "max mocks per test file")
	return func(inv Invocation) (verdict.Result, error) {
		if env := os.Getenv("BUDGET"); env != "" {
			if n, e := fmt.Sscanf(env, "%d", budget); n == 1 && e == nil {
				// budget set from env
			}
		}
		o, err := mockbudget.Check(mockbudget.Config{TargetDir: *dir, Budget: *budget})
		if err != nil {
			return Fail, err
		}
		if !o.Pass {
			inv.Stderr.Printf("[mock-budget] FAIL: %d file(s) over budget:\n", len(o.Violations))
			for _, v := range o.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			inv.Stderr.Println("[mock-budget] design smell — refactor/split OR add: // mock-budget: N — <reason>")
			return Fail, nil
		}
		inv.Stdout.Printf("[mock-budget] PASS — all test files within budget (%d mocks/file)\n", *budget)
		return Pass, nil
	}
}
