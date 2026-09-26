// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/taskfileinterface"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "taskfile-interface",
		Summary: "Validates the Taskfile public-interface contract.",
		Setup:   setupTaskfileInterface,
	})
}

func setupTaskfileInterface(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := taskfileinterface.Check(taskfileinterface.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass {
			inv.Stderr.Println("taskfile-interface: FAIL — public Taskfile command(s) missing:")
			for _, m := range outcome.Missing {
				inv.Stderr.Printf("  - %s\n", m)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Action: define these in Taskfile.yml or an included taskfiles/*.yml.")
			inv.Stderr.Println("If intentionally renamed/removed, update ExpectedCommands in")
			inv.Stderr.Println("cmd/loopzero-ops/internal/quality/taskfileinterface/check.go AND CI consumers.")
			return Fail, nil
		}
		inv.Stdout.Printf("taskfile-interface: PASS — all %d public commands resolve\n", len(taskfileinterface.ExpectedCommands))
		return Pass, nil
	}
}
