// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

const qualityHelpHeader = `loopzero-ops quality — quality gate orchestrator

USAGE:
  loopzero-ops quality <gate> [flags]

INTROSPECTION:
  --list          Print the gate inventory as NAME<TAB>TASKS<TAB>SUMMARY, sorted
                  and byte-stable (the M9 gate-set oracle). TASKS = the comma-
                  joined sorted taskfiles/quality.yml task(s) invoking the gate
                  (e.g. "gates", "diff", "diff,gates"), or "-" if none. Per-task
                  attribution: a silent un-wire from quality:gates flips the cell.
                  Flags:
                    --names         names only, one per line (for a per-gate loop)
                    --json          JSON array of {name, tasks, summary}
                    --taskfile PATH taskfile scanned for the wired bit
                                    (default: "taskfiles/quality.yml")
`

// qualityHelpText renders the help: the static header + a GATES inventory
// GENERATED from gateRegistry (single source of truth). The former
// hand-maintained per-gate list drifted from the registry — this cannot
// (M27 DRY: dry-second-doc-source). `--list` shows tasks; `<gate> --help`
// shows per-gate flags where supported.
func qualityHelpText() string {
	var b strings.Builder
	b.WriteString(qualityHelpHeader)
	b.WriteString("\n\nGATES (--list for tasks; <gate> --help for flags):\n")
	names := make([]string, 0, len(gateRegistry))
	sum := map[string]string{}
	for _, g := range gateRegistry {
		names = append(names, g.Name)
		sum[g.Name] = g.Summary
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&b, "  %s\n      %s\n", n, sum[n])
	}
	return b.String()
}

func runQuality(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, qualityHelpText())
		return 1
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Print(qualityHelpText())
		return 0
	case "--list":
		return runQualityList(args[1:])
	}
	if g, ok := gateDispatch()[args[0]]; ok {
		return g.Run(args[1:])
	}
	fmt.Fprintf(os.Stderr, "unknown gate: %s\n\n%s", args[0], qualityHelpText())
	return 1
}
