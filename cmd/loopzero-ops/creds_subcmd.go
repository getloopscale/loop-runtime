// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// creds subcommand — the operator's credentials file, as the environment variables the
// Helm renders read.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/credshape"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/seed"
)

const credsHelpText = `loopzero-ops creds — the operator's credentials file as shell exports

USAGE:
  eval "$(loopzero-ops creds env)"

Reads ~/.loopzero/dev-credentials.json (or LOOPZERO_DEV_CREDENTIALS), the file the seeds read,
and prints one export per credential the Kratos render needs:

  LOOPZERO_GITHUB_CLIENT_ID  LOOPZERO_GITHUB_CLIENT_SECRET
  LOOPZERO_GOOGLE_CLIENT_ID  LOOPZERO_GOOGLE_CLIENT_SECRET
  LOOPZERO_RESEND_API_KEY

A variable the environment already sets is left out: this fills gaps and never
overrides. Empty and placeholder values are left out. No file prints nothing and exits
0; a file that is not mode 0600 or not valid JSON exits 1.
`

func runCredsMain(args []string) int {
	path, err := credsFilePath()
	if err != nil {
		return emit(os.Stdout, os.Stderr, 1, "", fmt.Sprintf("loopzero-ops creds: %v\n", err))
	}
	code, out, msg := credsResult(args, path, os.LookupEnv)
	return emit(os.Stdout, os.Stderr, code, out, msg)
}

// credsFilePath resolves the file exactly as the seed subcommand does.
func credsFilePath() (string, error) {
	if p := os.Getenv("LOOPZERO_DEV_CREDENTIALS"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate dev-credentials.json: set LOOPZERO_DEV_CREDENTIALS: %w", err)
	}
	return filepath.Join(home, ".loopzero", "dev-credentials.json"), nil
}

// credsResult computes the exit code, the stdout payload and the stderr message.
func credsResult(args []string, path string, lookup func(string) (string, bool)) (int, string, string) {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		return 0, credsHelpText, ""
	}
	if len(args) != 1 || args[0] != "env" {
		return 1, "", "loopzero-ops creds: expected `env`\n\n" + credsHelpText
	}
	creds, err := seed.LoadCreds(path)
	if errors.Is(err, seed.ErrCredsAbsent) {
		return 0, "", ""
	}
	if err != nil {
		return 1, "", fmt.Sprintf("loopzero-ops creds env: %v\n", err)
	}
	vars := map[string]string{
		"LOOPZERO_GITHUB_CLIENT_ID":     creds.GitHub.ClientID,
		"LOOPZERO_GITHUB_CLIENT_SECRET": creds.GitHub.ClientSecret,
		"LOOPZERO_GOOGLE_CLIENT_ID":     creds.Google.ClientID,
		"LOOPZERO_GOOGLE_CLIENT_SECRET": creds.Google.ClientSecret,
		"LOOPZERO_RESEND_API_KEY":       creds.Resend.APIKey,
	}
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		value := strings.TrimSpace(vars[name])
		if value == "" || credshape.IsPlaceholder(value) {
			continue
		}
		if _, set := lookup(name); set {
			continue
		}
		fmt.Fprintf(&b, "export %s=%s\n", name, shellQuote(value))
	}
	return 0, b.String(), ""
}

// shellQuote single-quotes a value so a shell reads it as data: every quote inside it
// closes the string, emits an escaped quote, and reopens.
func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}
