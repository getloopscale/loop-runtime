// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/credshape"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/seed"
)

const credsHelpText = `loop-runtime-ops creds — the operator's credentials file as shell exports

USAGE:
  eval "$(loop-runtime-ops creds env)"

Reads ~/.loop-runtime/dev-credentials.json (or LOOP_RUNTIME_DEV_CREDENTIALS), the file the seeds read,
and prints one export per credential the Kratos render needs:

  LOOP_RUNTIME_GITHUB_CLIENT_ID  LOOP_RUNTIME_GITHUB_CLIENT_SECRET
  LOOP_RUNTIME_GOOGLE_CLIENT_ID  LOOP_RUNTIME_GOOGLE_CLIENT_SECRET
  LOOP_RUNTIME_RESEND_API_KEY     CLOUDFLARE_API_TOKEN (the edge's DNS-01 and dev-public:dns)
  LOOP_RUNTIME_OPENAI_API_KEY     LOOP_RUNTIME_DATA_PLANE_ADMIN_BOOTSTRAP_TOKEN (the data plane's Secrets)

A variable the environment already sets is left out: this fills gaps and never
overrides. Empty and placeholder values are left out. No file prints nothing and exits
0; a file that is not mode 0600 or not valid JSON exits 1.
`

func runCredsMain(args []string) int {
	path, err := credsFilePath()
	if err != nil {
		return emit(os.Stdout, os.Stderr, 1, "", fmt.Sprintf("loop-runtime-ops creds: %v\n", err))
	}
	code, out, msg := credsResult(args, path, os.LookupEnv)
	return emit(os.Stdout, os.Stderr, code, out, msg)
}

// credsFilePath resolves the file exactly as the seed subcommand does.
func credsFilePath() (string, error) {
	if p := os.Getenv("LOOP_RUNTIME_DEV_CREDENTIALS"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate dev-credentials.json: set LOOP_RUNTIME_DEV_CREDENTIALS: %w", err)
	}
	return filepath.Join(home, ".loop-runtime", "dev-credentials.json"), nil
}

// credsResult computes the exit code, the stdout payload and the stderr message.
func credsResult(args []string, path string, lookup func(string) (string, bool)) (int, string, string) {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		return 0, credsHelpText, ""
	}
	if len(args) != 1 || args[0] != "env" {
		return 1, "", "loop-runtime-ops creds: expected `env`\n\n" + credsHelpText
	}
	creds, err := seed.LoadCreds(path)
	if errors.Is(err, seed.ErrCredsAbsent) {
		return 0, "", ""
	}
	if err != nil {
		return 1, "", fmt.Sprintf("loop-runtime-ops creds env: %v\n", err)
	}
	vars := map[string]string{
		"LOOP_RUNTIME_GITHUB_CLIENT_ID":     creds.GitHub.ClientID,
		"LOOP_RUNTIME_GITHUB_CLIENT_SECRET": creds.GitHub.ClientSecret,
		"LOOP_RUNTIME_GOOGLE_CLIENT_ID":     creds.Google.ClientID,
		"LOOP_RUNTIME_GOOGLE_CLIENT_SECRET": creds.Google.ClientSecret,
		"LOOP_RUNTIME_RESEND_API_KEY":       creds.Resend.APIKey,
		"CLOUDFLARE_API_TOKEN":              creds.Cloudflare.APIToken,
		"LOOP_RUNTIME_OPENAI_API_KEY":       creds.OpenAI.APIKey,

		"LOOP_RUNTIME_DATA_PLANE_ADMIN_BOOTSTRAP_TOKEN": creds.DataPlane.AdminBootstrapToken,
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
