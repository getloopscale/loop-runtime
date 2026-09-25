// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testtargets"
)

func init() {
	register(Gate{
		Name:    "test-targets",
		Summary: "Verifies Taskfile test targets exist and are wired.",
		Setup:   setupTestTargets,
	})
}

func setupTestTargets(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		o, err := testtargets.Check(testtargets.Config{Root: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !o.Pass {
			msg := fmt.Sprintf("%d *_test.sh target(s) point at a missing repo path. "+
				"A relocated/deleted script-under-test left its test pointing at a dead "+
				"SCRIPT=/SCRIPT_UNDER_TEST= path (the dr/ relocation rot, finding "+
				"2026-05-22). Repoint the test to the new path or delete the orphan test.",
				len(o.Violations))
			if os.Getenv("GITHUB_ACTIONS") != "" {
				fmt.Fprintf(inv.Stdout, "::error::%s\n", msg)
			} else {
				fmt.Fprintf(inv.Stderr, "[test-targets] BLOCK: %s\n", msg)
			}
			for _, v := range o.Violations {
				fmt.Fprintf(inv.Stderr, "  %s\n", v)
			}
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "[test-targets] all %d test target(s) resolve — OK.\n", o.Checked)
		return Pass, nil
	}
}
