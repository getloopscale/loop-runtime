// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// sql subcommand — seed scripts rendered once, for scripts that each own a
// different transport to Postgres.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/getloopscale/loop-runtime/infra/scripts/bootstrapuser"
)

const sqlHelpText = `loopruntime-ops sql — render a seed script for psql

USAGE:
  loopruntime-ops sql bootstrap-user --user-id ID --email E --display-name N
                              --username U --account-id A --tenant-id T --role R

SCRIPTS:
  bootstrap-user   The JWT subject exists, owns an account, and is a member of
                   exactly ONE tenant (every other membership is removed). The
                   script is idempotent and ordered for the foreign keys.

The script goes to stdout; pipe it into psql or pass it with -c. Nothing is
printed when a field is missing or invalid (exit 1).
`

func runSQL(args []string, stdout, stderr io.Writer) int {
	code, out, msg := sqlResult(args)
	return emit(stdout, stderr, code, out, msg)
}

// sqlResult computes the exit code, the script and the stderr message.
func sqlResult(args []string) (int, string, string) {
	if len(args) == 0 {
		return 1, "", sqlHelpText
	}
	switch args[0] {
	case "--help", "-h", "help":
		return 0, sqlHelpText, ""
	case "bootstrap-user":
		return sqlBootstrapUser(args[1:])
	default:
		return 1, "", fmt.Sprintf("unknown sql script: %s\n\n%s", args[0], sqlHelpText)
	}
}

func sqlBootstrapUser(args []string) (int, string, string) {
	fs := flag.NewFlagSet("sql bootstrap-user", flag.ContinueOnError)
	var usage strings.Builder
	fs.SetOutput(&usage)
	var u bootstrapuser.User
	fs.StringVar(&u.ID, "user-id", "", "users.id — the JWT subject")
	fs.StringVar(&u.Email, "email", "", "users.email")
	fs.StringVar(&u.DisplayName, "display-name", "", "users.display_name and the account name")
	fs.StringVar(&u.Username, "username", "", "users.username")
	fs.StringVar(&u.AccountID, "account-id", "", "accounts.id created when the user owns no account")
	fs.StringVar(&u.TenantID, "tenant-id", "", "the only tenant the user stays a member of")
	fs.StringVar(&u.Role, "role", "", "owner | admin | member")
	if err := fs.Parse(args); err != nil {
		return 1, "", usage.String()
	}
	if fs.NArg() != 0 {
		return 1, "", fmt.Sprintf("loopruntime-ops sql bootstrap-user: unexpected argument %q\n", fs.Arg(0))
	}
	script, err := u.SQL()
	if err != nil {
		return 1, "", fmt.Sprintf("loopruntime-ops sql bootstrap-user: %v\n", err)
	}
	return 0, script, ""
}

func runSQLMain(args []string) int { return runSQL(args, os.Stdout, os.Stderr) }
