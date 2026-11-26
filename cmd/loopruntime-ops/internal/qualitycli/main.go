// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"fmt"
	"io"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/cliout"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/exitcode"
)

const helpHeader = `loopruntime-ops quality — quality gate orchestrator

USAGE:
  loopruntime-ops quality <gate> [--root DIR] [flags]

INTROSPECTION:
  --list          Print the gate inventory as NAME<TAB>TASKS<TAB>SUMMARY, sorted
                  and byte-stable (the gate-set oracle). TASKS = the comma-
                  joined sorted taskfiles/quality.yml task(s) invoking the gate
                  (e.g. "gates", "diff", "diff,gates"), or "-" if none. Per-task
                  attribution: a silent un-wire from quality:gates flips the cell.
                  Flags:
                    --names         names only, one per line (for a per-gate loop)
                    --json          JSON array of {name, tasks, summary}
                    --taskfile PATH taskfile scanned for the wired bit
                                    (default: "taskfiles/quality.yml")

EXIT (every gate):
  0 pass    1 usage error or a violation    5 the check could not complete
`

// HelpText renders the help: the static header plus the GATES inventory
// generated from the registry, so it cannot drift from what is registered.
func HelpText() string {
	var b strings.Builder
	b.WriteString(helpHeader)
	b.WriteString("\nGATES (--list for tasks; <gate> --help for flags):\n")
	for _, g := range Gates() {
		fmt.Fprintf(&b, "  %s\n      %s\n", g.Name, g.Summary)
	}
	return b.String()
}

// Main runs `loopruntime-ops quality <args>`.
func Main(args []string, stdout, stderr io.Writer) int {
	out, errOut := cliout.New(stdout), cliout.New(stderr)
	return cliout.Settle(dispatch(args, out, errOut), out, errOut)
}

func dispatch(args []string, stdout, stderr *cliout.Writer) int {
	if len(args) == 0 {
		stderr.Print(HelpText())
		return exitcode.Usage
	}
	switch args[0] {
	case "--help", "-h", "help":
		stdout.Print(HelpText())
		return exitcode.OK
	case "--list":
		return runList(stdout, stderr, args[1:])
	}
	if g, ok := Lookup(args[0]); ok {
		return g.run(args[1:], stdout, stderr)
	}
	stderr.Printf("unknown gate: %s\n\n%s", args[0], HelpText())
	return exitcode.Usage
}
