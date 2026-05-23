// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Command loopzero-ops is the operator toolbox for LoopZero platform CI gates.
// Phase 0 T0.1 of plan docs/plans/2026-05-22-shell-script-strategic-plan.md
// lands this minimal skeleton with a single subcommand: `quality scripts-audit`.
//
// Future subcommands (Phase 5/6/7 of plan): quality no-new-bash,
// quality no-shell-wrap, quality pluto, quality opencost.
//
// Exit codes (CLAUDE.md Rule 4 — PERMANENT):
//   0 — success
//   1 — usage error
//   2 — build failure (subcommand-emitted)
//   3 — deploy failure (subcommand-emitted)
//   4 — auth error (subcommand-emitted)
//   5 — partial failure (subcommand-emitted)
package main

import (
	"fmt"
	"os"
)

const helpText = `loopzero-ops — LoopZero operator toolbox (Phase 0 minimal skeleton)

USAGE:
  loopzero-ops <subcommand> [flags]

SUBCOMMANDS:
  quality <gate> [flags]               Run a CI quality gate (see quality --help)
  pulumi archive [flags]               Export Pulumi stack state to S3 archive

EXIT CODES (PERMANENT — CLAUDE.md Rule 4):
  0  success
  1  usage error
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, helpText)
		return 1
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Print(helpText)
		return 0
	case "quality":
		return runQuality(args[1:])
	case "pulumi":
		return runPulumi(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n%s", args[0], helpText)
		return 1
	}
}
