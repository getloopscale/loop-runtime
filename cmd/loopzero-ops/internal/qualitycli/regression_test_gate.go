// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/regressiontest"
)

func init() {
	register(Gate{
		Name:    "regression-test",
		Summary: "Asserts a regression test exists for tracked fixed bugs.",
		// Reads PR_TITLE / PR_BODY / PR_COMMITS / PR_DIFF_FILES from the environment
		// and SKIPs without them, which is always the case in a repo scan: it is
		// run by the caller that supplies the PR context.
		Wiring: parameterized,
		NoRoot: true,
		Setup:  setupRegressionTest,
	})
}

func setupRegressionTest(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		o := regressiontest.Check(regressiontest.Config{
			Title:     os.Getenv("PR_TITLE"),
			Body:      os.Getenv("PR_BODY"),
			Commits:   os.Getenv("PR_COMMITS"),
			DiffFiles: os.Getenv("PR_DIFF_FILES"),
		})
		if o.Skipped {
			fmt.Fprintln(inv.Stdout, "[regression-gate] no bug-fix references — SKIP gate")
			return Pass, nil
		}
		if len(o.IssueRefs) > 0 {
			fmt.Fprintln(inv.Stdout, "[regression-gate] detected bug-fix references:")
			for _, r := range o.IssueRefs {
				fmt.Fprintf(inv.Stdout, "  %s\n", r)
			}
		}
		if !o.Pass {
			fmt.Fprintf(inv.Stderr, "[regression-gate] FAIL: %s\n", o.Reason)
			fmt.Fprintln(inv.Stderr, "Add a regression test in a *_test.go or *.test.ts file referencing the bug.")
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "[regression-gate] PASS — test file found in diff")
		return Pass, nil
	}
}
