// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/testtargets"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "test-targets",
		Summary: "Verifies Taskfile test targets exist and are wired.",
		Setup:   setupTestTargets,
	})
}

func setupTestTargets(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		o, err := testtargets.Check(testtargets.Config{Root: inv.Root})
		if err != nil {
			return nil, err
		}
		if o.Verdict() == verdict.Fail {
			msg := fmt.Sprintf("%d *_test.sh target(s) point at a missing repo path. "+
				"A relocated/deleted script-under-test left its test pointing at a dead "+
				"SCRIPT=/SCRIPT_UNDER_TEST= path (the dr/ relocation rot, finding "+
				"2026-05-22). Repoint the test to the new path or delete the orphan test.",
				len(o.Violations))
			if os.Getenv("GITHUB_ACTIONS") != "" {
				inv.Stdout.Printf("::error::%s\n", msg)
			} else {
				inv.Stderr.Printf("[test-targets] BLOCK: %s\n", msg)
			}
			for _, v := range o.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			return o, nil
		}
		inv.Stdout.Printf("[test-targets] all %d test target(s) resolve — OK.\n", o.Checked)
		return o, nil
	}
}
