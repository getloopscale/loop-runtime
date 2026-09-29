// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCredsFile writes a dev-credentials.json with mode 0600, the only mode LoadCreds
// accepts.
func writeCredsFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dev-credentials.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write creds: %v", err)
	}
	return path
}

const fullCreds = `{
  "github": {"client_id": "gh-id", "client_secret": "gh-secret"},
  "google": {"client_id": "g-id", "client_secret": "g-secret"},
  "resend": {"api_key": "re_key"}
}`

func credsEnvOutput(t *testing.T, path string, env map[string]string) (int, string, string) {
	t.Helper()
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	return credsResult([]string{"env"}, path, lookup)
}

// Kratos renders its OAuth providers and its mail transport from these variables, and
// the operator keeps their values in the same file the seeds read. Rendering from
// that file is what stops dev-public from installing Kratos with REPLACE_VIA_*
// placeholders when no environment file carries them (#445).
func TestCredsEnvExportsTheKratosVariablesFromTheCredentialsFile(t *testing.T) {
	code, out, msg := credsEnvOutput(t, writeCredsFile(t, fullCreds), nil)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, msg)
	}
	for _, want := range []string{
		"export LOOPZERO_GITHUB_CLIENT_ID='gh-id'",
		"export LOOPZERO_GITHUB_CLIENT_SECRET='gh-secret'",
		"export LOOPZERO_GOOGLE_CLIENT_ID='g-id'",
		"export LOOPZERO_GOOGLE_CLIENT_SECRET='g-secret'",
		"export LOOPZERO_RESEND_API_KEY='re_key'",
	} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// A value the operator set explicitly wins over the file: this only fills gaps.
func TestCredsEnvNeverOverridesAVariableAlreadySet(t *testing.T) {
	_, out, _ := credsEnvOutput(t, writeCredsFile(t, fullCreds), map[string]string{"LOOPZERO_GITHUB_CLIENT_ID": "from-env"})
	if strings.Contains(out, "LOOPZERO_GITHUB_CLIENT_ID") {
		t.Errorf("exported LOOPZERO_GITHUB_CLIENT_ID although the environment already sets it:\n%s", out)
	}
}

// A placeholder is not a credential: exporting it would render the same broken Kratos.
func TestCredsEnvSkipsEmptyAndPlaceholderValues(t *testing.T) {
	_, out, _ := credsEnvOutput(t, writeCredsFile(t, `{"github": {"client_id": "", "client_secret": "REPLACE_VIA_PULUMI"}, "resend": {"api_key": "re_key"}}`), nil)
	if strings.Contains(out, "LOOPZERO_GITHUB_CLIENT") {
		t.Errorf("exported an empty or placeholder value:\n%s", out)
	}
	if !strings.Contains(out, "export LOOPZERO_RESEND_API_KEY='re_key'\n") {
		t.Errorf("dropped a real value:\n%s", out)
	}
}

// The output is evaluated by a shell, so a quote inside a value must stay data.
func TestCredsEnvQuotesValuesForTheShell(t *testing.T) {
	_, out, _ := credsEnvOutput(t, writeCredsFile(t, `{"resend": {"api_key": "a'b$(x)"}}`), nil)
	if want := `export LOOPZERO_RESEND_API_KEY='a'\''b$(x)'` + "\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// No file is a supported state (standalone runs without one): nothing to export.
func TestCredsEnvPrintsNothingWhenTheFileIsAbsent(t *testing.T) {
	code, out, _ := credsEnvOutput(t, filepath.Join(t.TempDir(), "absent.json"), nil)
	if code != 0 || out != "" {
		t.Errorf("exit %d, output %q — an absent file exports nothing and succeeds", code, out)
	}
}

// A readable-by-others or malformed file fails loudly rather than exporting nothing.
func TestCredsEnvFailsOnAFileWithTheWrongMode(t *testing.T) {
	path := writeCredsFile(t, fullCreds)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := credsEnvOutput(t, path, nil)
	if code == 0 || out != "" {
		t.Errorf("exit %d, output %q — a 0644 credentials file must be refused", code, out)
	}
}

// The dev-public edge issues its wildcard certificates by DNS-01 and dev-public:dns
// checks the zone, both with the operator's Cloudflare token — kept in this same file.
// Nothing exported it, so every cleanroom's DNS verification failed its precondition (#455).
func TestCredsEnvExportsTheCloudflareToken(t *testing.T) {
	path := writeCredsFile(t, `{"cloudflare": {"api_token": "cf-token"}}`)
	code, out, msg := credsEnvOutput(t, path, nil)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, msg)
	}
	if !strings.Contains(out, "export CLOUDFLARE_API_TOKEN='cf-token'\n") {
		t.Errorf("output lacks the Cloudflare token export:\n%s", out)
	}
}
