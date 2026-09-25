// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"errors"
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testtargetcoverage"
)

func init() {
	register(Gate{
		Name:    "test-target-coverage",
		Summary: "Every PACKAGE with tests is run by a Taskfile target, or carries a NAMED exemption with a reason and a sunset. Package granularity, not module: a `./unit/...` marked the whole module covered while 41 files in three subtrees never ran (B-114). `go build` does not count (B-087/B-088).",
		Setup:   setupTestTargetCoverage,
	})
}

func setupTestTargetCoverage(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		out, err := testtargetcoverage.Check(testtargetcoverage.Config{RepoRoot: inv.Root})
		switch {
		case errors.Is(err, testtargetcoverage.ErrModuleUncovered):
			fmt.Fprintf(inv.Stderr, "test-target-coverage: FAIL — %d module(s) with no Taskfile target running their tests:\n", len(out.Uncovered))
			for _, m := range out.Uncovered {
				fmt.Fprintf(inv.Stderr, "  %s\n", m)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "Add `cd <module> && go test ./...` to taskfiles/test.yml (GOWORK=off for")
			fmt.Fprintln(inv.Stderr, "modules outside go.work), OR add a NAMED exemption with a reason in")
			fmt.Fprintln(inv.Stderr, "internal/quality/testtargetcoverage. `go build` does not count: that is")
			fmt.Fprintln(inv.Stderr, "exactly how cmd/loopzero-ops looked covered while no test of it ever ran (B-088).")
			return Fail, nil
		case errors.Is(err, testtargetcoverage.ErrPackageUncovered):
			fmt.Fprintf(inv.Stderr, "test-target-coverage: FAIL — %d package(s) with tests that no target runs:\n", len(out.UncoveredPackages))
			for _, p := range out.UncoveredPackages {
				fmt.Fprintf(inv.Stderr, "  %s\n", p)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "The MODULE has a target; it is the target that does not reach these subtrees. Widen")
			fmt.Fprintln(inv.Stderr, "the target's pattern, give them a target of their own, or declare a PACKAGE exemption")
			fmt.Fprintln(inv.Stderr, "with a reason and a sunset. This is exactly how 41 files went unseen in a module the")
			fmt.Fprintln(inv.Stderr, "gate reported as covered (B-092/B-114).")
			return Fail, nil
		case err != nil:
			return Fail, err
		}
		fmt.Fprintf(inv.Stdout, "test-target-coverage: PASS — %d module(s), %d covered, %d exempt with a dated reason. "+
			"The granularity is the PACKAGE: a module with a target still fails if its pattern does not reach "+
			"a subtree that has tests (B-114).\n",
			len(out.Modules), len(out.Covered), len(out.Exempt))
		return Pass, nil
	}
}
