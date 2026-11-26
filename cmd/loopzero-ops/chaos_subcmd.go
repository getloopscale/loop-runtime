// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// chaos subcommand group — pure-logic pieces extracted from the chaos runner
// bash (Track C TC3.1). Only the SLO verdict arithmetic lives here; the
// chaos-mesh injection (kubectl apply) and LIVE sampling stay bash (ADR D1/D4).
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/chaos/slo"
)

const chaosHelpText = `loopzero-ops chaos — pure-logic helpers for the chaos resilience runner

USAGE:
  loopzero-ops chaos <subcommand> [flags]

SUBCOMMANDS:
  slo-compare <baseline.json> <during.json> <post.json>
        Decide PASS/FAIL/INCONCLUSIVE from chaos samples (replaces the verdict
        arithmetic in chaos/run-scenario.sh:235-259). Exit 0=PASS 2=FAIL
        5=INCONCLUSIVE (run-full.sh treats non-0/2 as INCONCLUSIVE).
`

func runChaos(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, chaosHelpText)
		return 1
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Print(chaosHelpText)
		return 0
	case "slo-compare":
		return runChaosSLOCompare(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown chaos subcommand: %s\n\n%s", args[0], chaosHelpText)
		return 1
	}
}

func runChaosSLOCompare(args []string) int {
	fs := flag.NewFlagSet("chaos slo-compare", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 3 {
		fmt.Fprintln(os.Stderr, "chaos slo-compare: requires 3 args: <baseline.json> <during.json> <post.json>")
		return 1
	}
	samples := make([]slo.Sample, 3)
	for i, path := range []string{fs.Arg(0), fs.Arg(1), fs.Arg(2)} {
		data, err := os.ReadFile(path) // #nosec G304 -- operator-supplied sample path
		if err != nil {
			fmt.Fprintf(os.Stderr, "chaos slo-compare: read %s: %v\n", path, err)
			return 5 // unreadable input → INCONCLUSIVE (re-run)
		}
		s, err := slo.ParseSample(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "chaos slo-compare: parse %s: %v\n", path, err)
			return 5
		}
		samples[i] = s
	}
	v := slo.Compare(samples[0], samples[1], samples[2], slo.DefaultThresholds())
	fmt.Printf("chaos slo-compare: %s\n", v.Result)
	for _, r := range v.Reasons {
		fmt.Printf("  - %s\n", r)
	}
	return v.Result.ExitCode()
}
