// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// patchKratosScriptPath resolves the script independently of the helper
// from other tests (each test file is self-contained — no shared package
// state).
func patchKratosScriptPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "scripts", "local", "patch-kratos-dev-public.sh"))
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("script not found at %s: %v", p, err)
	}
	return p
}

func readPatchKratosScript(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(patchKratosScriptPath(t))
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	return string(b)
}

// TestPatchKratosDevPublic_FallsBackToDevCredentials locks Finding #3 of
// issue #91 (Cleanroom mandate canonical chain run 2026-06-18).
//
// Before this fix, load_env consulted (1) env var → (2) ENV_FILE
// (operator-specific path /home/paulo/loopzero/.env). When neither was
// populated the script exited 1 with "LOOPZERO_GITHUB_CLIENT_ID not set in
// env or .env" — opaque failure mode for first-time operators who had
// the canonical credentials in ~/.loopzero/dev-credentials.json but didn't
// know they needed to export them to env or copy them to a .env file.
//
// Sponsor LIVE-caught 2026-06-18: fresh Cleanroom -> Step 4 patch-
// kratos failed because env vars unset -> Step 3 admin login also
// failed (chain).
//
// Canonical fix: load_env adds a third fallback layer that reads
// ~/.loopzero/dev-credentials.json (the canonical source per memories
// feedback_post_cleanroom_seed_github_app_canonical +
// reference_admin_cli_hydra_client_canonical), mapping the required
// LOOPZERO_* env keys to their JSON paths:
//
//	LOOPZERO_GITHUB_CLIENT_ID     -> github.client_id
//	LOOPZERO_GITHUB_CLIENT_SECRET -> github.client_secret
//	LOOPZERO_GOOGLE_CLIENT_ID     -> google.client_id
//	LOOPZERO_GOOGLE_CLIENT_SECRET -> google.client_secret
//	LOOPZERO_RESEND_API_KEY       -> resend.api_key
//
// The fallback ORDER is: env var > ENV_FILE > DEV_CREDS_FILE > exit 1.
// This preserves explicit-override semantics (operators can still set
// env vars OR populate .env to override the canonical fallback).
func TestPatchKratosDevPublic_FallsBackToDevCredentials(t *testing.T) {
	body := readPatchKratosScript(t)

	// T1. DEV_CREDS_FILE default points at ~/.loopzero/dev-credentials.json
	// (the canonical post-Cleanroom seed file).
	devCredsRe := regexp.MustCompile(`DEV_CREDS_FILE.*\$HOME/\.loopzero/dev-credentials\.json`)
	if !devCredsRe.MatchString(body) {
		t.Error("DEV_CREDS_FILE default not set to $HOME/.loopzero/dev-credentials.json — canonical fallback missing")
	}

	// T2. load_env references DEV_CREDS_FILE as a fallback path AFTER
	// ENV_FILE. Pattern: the function body should contain `DEV_CREDS_FILE`
	// in a conditional read.
	loadEnvRe := regexp.MustCompile(`(?s)load_env\(\) \{(.*?)\n\}`)
	m := loadEnvRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("load_env function not found — structure changed")
	}
	loadEnvBody := m[1]

	if !strings.Contains(loadEnvBody, "DEV_CREDS_FILE") {
		t.Error("load_env does not consult DEV_CREDS_FILE — Finding #3 fallback missing")
	}

	// T3. The fallback ORDER is env > ENV_FILE > DEV_CREDS_FILE. Check
	// that DEV_CREDS_FILE appears AFTER ENV_FILE in the function body.
	envFileIdx := strings.Index(loadEnvBody, "ENV_FILE")
	devCredsIdx := strings.Index(loadEnvBody, "DEV_CREDS_FILE")
	if envFileIdx < 0 || devCredsIdx < 0 || envFileIdx > devCredsIdx {
		t.Error("DEV_CREDS_FILE fallback must appear AFTER ENV_FILE in load_env (Finding #3 order)")
	}

	// T4. All 5 required env keys have a mapping in the sibling Python
	// helper (patch_kratos_load_devcreds.py). Defensive: catches a
	// future refactor that adds an env key without updating the
	// fallback mapping. Reading the helper is canonical per
	// un-homed-logic gate (reimplementable logic lives in Python
	// helper, NOT inline bash heredoc).
	helperPath, err := filepath.Abs(filepath.Join("..", "..", "scripts", "local", "patch_kratos_load_devcreds.py"))
	if err != nil {
		t.Fatalf("resolve helper path: %v", err)
	}
	helperBytes, err := os.ReadFile(helperPath)
	if err != nil {
		t.Fatalf("read helper %s: %v — Finding #3 fallback helper missing", helperPath, err)
	}
	helperBody := string(helperBytes)

	// T4a. load_env invokes the helper.
	if !strings.Contains(loadEnvBody, "patch_kratos_load_devcreds.py") {
		t.Error("load_env does not invoke patch_kratos_load_devcreds.py helper — Finding #3 fallback not wired")
	}

	// T4b. Helper covers all 5 required LOOPZERO_* keys.
	requiredKeys := []string{
		"LOOPZERO_GITHUB_CLIENT_ID",
		"LOOPZERO_GITHUB_CLIENT_SECRET",
		"LOOPZERO_GOOGLE_CLIENT_ID",
		"LOOPZERO_GOOGLE_CLIENT_SECRET",
		"LOOPZERO_RESEND_API_KEY",
	}
	for _, k := range requiredKeys {
		if !strings.Contains(helperBody, k) {
			t.Errorf("required key %q missing from helper MAPPING — operator with credentials only in dev-credentials.json cannot run patch-kratos", k)
		}
	}

	// T5. The error message mentions DEV_CREDS_FILE in addition to env
	// vars and ENV_FILE — operator with no credentials anywhere sees a
	// pointer to the canonical file.
	if !strings.Contains(loadEnvBody, "dev-credentials.json") &&
		!strings.Contains(loadEnvBody, "DEV_CREDS_FILE") {
		t.Error("load_env error message does not reference dev-credentials.json — operator triage harder")
	}

	// T6. Traceability comment cites Finding #3 / issue #91 so a future
	// "clean up" refactor does not remove the fallback.
	if !strings.Contains(body, "Finding #3") {
		t.Error("Finding #3 traceability comment missing — future refactor may remove the canonical fallback")
	}
}
