// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"errors"
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/testtargetcoverage"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "test-target-coverage",
		Summary: "Every PACKAGE with tests is run by a Taskfile target, or carries a NAMED exemption with a reason and a sunset. Package granularity, not module: a `./unit/...` marked the whole module covered while 41 files in three subtrees never ran (B-114). `go build` does not count (B-087/B-088).",
		Setup:   setupTestTargetCoverage,
	})
}

func setupTestTargetCoverage(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := testtargetcoverage.Check(testtargetcoverage.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		switch {
		case errors.Is(out.Violation, testtargetcoverage.ErrModuleUncovered):
			inv.Stderr.Printf("test-target-coverage: FAIL — %d module(s) with no Taskfile target running their tests:\n", len(out.Uncovered))
			for _, m := range out.Uncovered {
				inv.Stderr.Printf("  %s\n", m)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Add `cd <module> && go test ./...` to taskfiles/test.yml (GOWORK=off for")
			inv.Stderr.Println("modules outside go.work), OR add a NAMED exemption with a reason in")
			inv.Stderr.Println("internal/quality/testtargetcoverage. `go build` does not count: that is")
			inv.Stderr.Println("exactly how cmd/loop-runtime-ops looked covered while no test of it ever ran (B-088).")
			return out, nil
		case errors.Is(out.Violation, testtargetcoverage.ErrPackageUncovered):
			inv.Stderr.Printf("test-target-coverage: FAIL — %d package(s) with tests that no target runs:\n", len(out.UncoveredPackages))
			for _, p := range out.UncoveredPackages {
				inv.Stderr.Printf("  %s\n", p)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("The MODULE has a target; it is the target that does not reach these subtrees. Widen")
			inv.Stderr.Println("the target's pattern, give them a target of their own, or declare a PACKAGE exemption")
			inv.Stderr.Println("with a reason and a sunset. This is exactly how 41 files went unseen in a module the")
			inv.Stderr.Println("gate reported as covered (B-092/B-114).")
			return out, nil
		}
		inv.Stdout.Printf("test-target-coverage: PASS — %d module(s), %d covered, %d exempt with a dated reason. "+
			"The granularity is the PACKAGE: a module with a target still fails if its pattern does not reach "+
			"a subtree that has tests (B-114).\n",
			len(out.Modules), len(out.Covered), len(out.Exempt))
		return out, nil
	}
}
