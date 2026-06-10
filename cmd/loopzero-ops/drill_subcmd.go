// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// drill subcommand group — pure-logic pieces extracted from the drill/DR bash
// orchestrators (Track C of docs/plans/completed/2026-05-24-drill-consolidation-migration-plan.md).
// Only validation/arithmetic/shaping lives here; provisioning (pulumi/doctl/helm)
// stays in bash (ADR D1 no-shell-wrap).
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/loopzero/loopzero/pkg/quality/drillcost"
)

const drillHelpText = `loopzero-ops drill — pure-logic helpers for the drill/DR orchestrators

USAGE:
  loopzero-ops drill <subcommand> [flags]

SUBCOMMANDS:
  cost --start <ISO> --end <ISO>    Compute drill run cost + render markdown report
                                    (replaces the arithmetic half of dr/cost-report.sh;
                                    the --auto Pulumi-state read stays in bash)
`

func runDrill(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, drillHelpText)
		return 1
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Print(drillHelpText)
		return 0
	case "cost":
		return runDrillCost(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown drill subcommand: %s\n\n%s", args[0], drillHelpText)
		return 1
	}
}

// runDrillCost is the pure-logic cost report (replaces dr/cost-report.sh:106-159).
func runDrillCost(args []string) int {
	fs := flag.NewFlagSet("drill cost", flag.ContinueOnError)
	start := fs.String("start", "", "provision-start ISO timestamp (required)")
	end := fs.String("end", "", "teardown-end ISO timestamp (required)")
	output := fs.String("output", "", "write markdown to this file (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *start == "" || *end == "" {
		fmt.Fprintln(os.Stderr, "drill cost: --start AND --end required")
		return 1
	}
	rates := drillcost.DefaultRates()
	rep, err := drillcost.Compute(*start, *end, rates)
	if err != nil {
		fmt.Fprintf(os.Stderr, "drill cost: %v\n", err)
		return 1
	}
	md := rep.Markdown(rates)
	if *output != "" {
		if err := os.WriteFile(*output, []byte(md+"\n"), 0o644); err != nil { // #nosec G306 -- report artifact
			fmt.Fprintf(os.Stderr, "drill cost: write %s: %v\n", *output, err)
			return 5
		}
		fmt.Fprintf(os.Stderr, "[drill cost] report written: %s ($%.2f total)\n", *output, rep.CostTotal)
		return 0
	}
	fmt.Print(md)
	return 0
}
