// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/testdeletion"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
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
	return func(inv Invocation) (verdict.Result, error) {
		o, err := testdeletion.Check(testdeletion.Config{
			BaseRef:  os.Getenv("BASE_REF"),
			HeadRef:  os.Getenv("HEAD_REF"),
			PRBody:   os.Getenv("PR_BODY"),
			PRLabels: os.Getenv("PR_LABELS"),
		})
		if err != nil {
			return nil, fmt.Errorf("the range could not be read: %w", err)
		}
		if len(o.DeletedTests) > 0 {
			inv.Stderr.Println("[test-deletion] TRUE test deletions detected:")
			for _, f := range o.DeletedTests {
				inv.Stderr.Printf("  %s\n", f)
			}
		}
		if o.Verdict() == verdict.Fail {
			inv.Stderr.Printf("[test-deletion] FAIL: %s\n", o.Reason)
			inv.Stderr.Println("Justify it in ONE of these — the commit body is the one that lives in the repo:")
			inv.Stderr.Println("  - a commit message in the range:  Test deletion justification: <reason>")
			inv.Stderr.Println("  - the PR body:                    Test deletion justification: <reason>")
			inv.Stderr.Println("  - the 'test-deletion-approved' PR label")
			return o, nil
		}
		inv.Stdout.Printf("[test-deletion] PASS — %s\n", o.Reason)
		return o, nil
	}
}
