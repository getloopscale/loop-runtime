// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testdeletion"
)

func init() {
	register(Gate{
		Name:    "test-deletion",
		Summary: "Guards deletion of test files against coverage loss.",
		NoRoot:  true,
		Setup:   setupTestDeletion,
	})
}

func setupTestDeletion(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		o, err := testdeletion.Check(testdeletion.Config{
			BaseRef:  os.Getenv("BASE_REF"),
			HeadRef:  os.Getenv("HEAD_REF"),
			PRBody:   os.Getenv("PR_BODY"),
			PRLabels: os.Getenv("PR_LABELS"),
		})
		if err != nil {
			return Fail, fmt.Errorf("the range could not be read: %w", err)
		}
		if len(o.DeletedTests) > 0 {
			fmt.Fprintln(inv.Stderr, "[test-deletion] TRUE test deletions detected:")
			for _, f := range o.DeletedTests {
				fmt.Fprintf(inv.Stderr, "  %s\n", f)
			}
		}
		if !o.Pass {
			fmt.Fprintf(inv.Stderr, "[test-deletion] FAIL: %s\n", o.Reason)
			fmt.Fprintln(inv.Stderr, "Justify it in ONE of these — the commit body is the one that lives in the repo:")
			fmt.Fprintln(inv.Stderr, "  - a commit message in the range:  Test deletion justification: <reason>")
			fmt.Fprintln(inv.Stderr, "  - the PR body:                    Test deletion justification: <reason>")
			fmt.Fprintln(inv.Stderr, "  - the 'test-deletion-approved' PR label")
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "[test-deletion] PASS — %s\n", o.Reason)
		return Pass, nil
	}
}
