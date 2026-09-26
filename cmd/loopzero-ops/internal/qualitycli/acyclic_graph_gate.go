// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/acyclicgraph"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "acyclic-graph",
		Summary: "Enforces the decomposition DAG (loopzero-contracts ◄── engine/cloud/cli); no import cycle.",
		Setup:   setupAcyclicGraph,
	})
}

// setupAcyclicGraph is the Go home for the acyclic-graph gate (M5 — plan
// m5-governance-durable). Enforces the decomposition DAG: loopzero-contracts imports
// nothing from the engine/cloud/cli; the engine never imports the cloud; the
// cloud never imports engine-internal pkg/*. Sibling repos are resolved relative
// to the engine root and skipped when absent.
func setupAcyclicGraph(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := acyclicgraph.Check(acyclicgraph.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("acyclic-graph: FAIL — %d forbidden upward import edge(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Decomposition invariant (ADP): loopzero-contracts ◄── engine + loopzero-cloud + loopzero-cli. Arrows never point at a more-stable node.")
			return outcome, nil
		}
		msg := fmt.Sprintf("acyclic-graph: PASS — graph is acyclic (checked: %s", strings.Join(outcome.Checked, ", "))
		if len(outcome.Skipped) > 0 {
			msg += fmt.Sprintf("; skipped absent: %s", strings.Join(outcome.Skipped, ", "))
		}
		inv.Stdout.Println(msg + ")")
		return outcome, nil
	}
}
