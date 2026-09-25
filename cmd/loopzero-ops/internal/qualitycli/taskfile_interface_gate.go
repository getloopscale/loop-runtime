// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/taskfileinterface"
)

func init() {
	register(Gate{
		Name:    "taskfile-interface",
		Summary: "Validates the Taskfile public-interface contract.",
		Setup:   setupTaskfileInterface,
	})
}

func setupTaskfileInterface(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := taskfileinterface.Check(taskfileinterface.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass {
			fmt.Fprintln(inv.Stderr, "taskfile-interface: FAIL — public Taskfile command(s) missing:")
			for _, m := range outcome.Missing {
				fmt.Fprintf(inv.Stderr, "  - %s\n", m)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "Action: define these in Taskfile.yml or an included taskfiles/*.yml.")
			fmt.Fprintln(inv.Stderr, "If intentionally renamed/removed, update ExpectedCommands in")
			fmt.Fprintln(inv.Stderr, "cmd/loopzero-ops/internal/quality/taskfileinterface/check.go AND CI consumers.")
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "taskfile-interface: PASS — all %d public commands resolve\n", len(taskfileinterface.ExpectedCommands))
		return Pass, nil
	}
}
