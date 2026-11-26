// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Command loop-runtime-ops is the operator toolbox for LoopRuntime platform CI gates.
// Phase 0 T0.1 of plan 2026-05-22-shell-script-strategic-plan
// lands this minimal skeleton with a single subcommand: `quality scripts-audit`.
//
// Future subcommands (Phase 5/6/7 of plan): quality no-new-bash,
// quality no-shell-wrap, quality pluto, quality opencost.
//
// Exit codes (CLAUDE.md Rule 4 — PERMANENT):
//
//	0 — success
//	1 — usage error
//	2 — build failure (subcommand-emitted)
//	3 — deploy failure (subcommand-emitted)
//	4 — auth error (subcommand-emitted)
//	5 — partial failure (subcommand-emitted)
package main

import (
	"fmt"
	"os"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/qualitycli"
)

const helpText = `loop-runtime-ops — LoopRuntime operator toolbox (Phase 0 minimal skeleton)

USAGE:
  loop-runtime-ops <subcommand> [flags]

SUBCOMMANDS:
  quality <gate> [flags]               Run a CI quality gate (see quality --help)
  pulumi archive [flags]               Export Pulumi stack state to S3 archive
  drill <subcommand> [flags]           Drill/DR pure-logic helpers (see drill --help)
  chaos <subcommand> [flags]           Chaos runner pure-logic helpers (see chaos --help)
  seed <name> [flags]                  Canonical post-Cleanroom seeds (see seed --help)
  dsn <field>                          Print one part of a Postgres DSN read on stdin
  creds env                            Print the credentials file as shell exports (Kratos inputs)
  sql <script> [flags]                 Render a seed script for psql (see sql --help)
  kubeconform-schemas [--check]        Derive the runtime.getloopscale.com kubeconform schemas from the CRDs
  validate-chart --release R --chart D Render one chart and schema-validate the render

EXIT CODES (every subcommand):
  0  success
  1  usage error, or it ran and the subject is wrong (a violation, a failed seed)
  5  it could not reach a verdict (an unreadable input, an unreachable target)
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
		return qualitycli.Main(args[1:], os.Stdout, os.Stderr)
	case "pulumi":
		return runPulumi(args[1:])
	case "drill":
		return runDrill(args[1:])
	case "chaos":
		return runChaos(args[1:])
	case "leak":
		return runLeak(args[1:])
	case "seed":
		return runSeed(args[1:])
	case "dsn":
		return runDSNMain(args[1:])
	case "creds":
		return runCredsMain(args[1:])
	case "sql":
		return runSQLMain(args[1:])
	case "kubeconform-schemas":
		return runKubeconformSchemasMain(args[1:])
	case "validate-chart":
		return runValidateChartMain(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n%s", args[0], helpText)
		return 1
	}
}
