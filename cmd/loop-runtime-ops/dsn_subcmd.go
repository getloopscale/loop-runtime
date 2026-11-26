// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// dsn subcommand — the one place a script reads a part of a Postgres DSN.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/dsn"
)

const dsnHelpText = `loop-runtime-ops dsn — read one part of a Postgres DSN

USAGE:
  printf '%s' "$DSN" | loop-runtime-ops dsn <field>

FIELDS:
  user | password | host | port | database

The DSN is read from stdin, never from an argument: an argument is visible to
every user on the host through the process list. The password is printed
decoded. A value that is not a postgres:// or postgresql:// URL, or a DSN
without the field, prints nothing and exits 1.
`

func runDSN(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	code, out, msg := dsnResult(args, stdin)
	return emit(stdout, stderr, code, out, msg)
}

// dsnResult computes the exit code, the stdout payload and the stderr message.
func dsnResult(args []string, stdin io.Reader) (int, string, string) {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		return 0, dsnHelpText, ""
	}
	if len(args) != 1 {
		return 1, "", "loop-runtime-ops dsn: exactly one field name expected; the DSN goes on stdin\n\n" + dsnHelpText
	}
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return 5, "", fmt.Sprintf("loop-runtime-ops dsn: read stdin: %v\n", err)
	}
	value, err := dsn.Field(strings.TrimSpace(string(raw)), args[0])
	if err != nil {
		return 1, "", fmt.Sprintf("loop-runtime-ops dsn %s: %v\n", args[0], err)
	}
	return 0, value + "\n", ""
}

func runDSNMain(args []string) int { return runDSN(args, os.Stdin, os.Stdout, os.Stderr) }
