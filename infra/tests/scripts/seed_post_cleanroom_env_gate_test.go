// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"regexp"
	"strings"
	"testing"
)

// scriptPath + readScript helpers live in vault_bootstrap_test.go
// (shared package-level helpers).

// TestSeedPostCleanroomAll_DevPublicStepsGatedByEnv locks the env-gating
// fix for issue #99: a step that configures something only dev-public has must
// SKIP, with a reason, in every other environment instead of failing there.
//
// The steps issue #99 was about — patch-kratos and hydra-urls — no longer exist:
// the Kratos and Hydra URLs they patched are environment values `helmfile sync`
// renders (principle_violations#67). The gate still guards the dev-public-only
// step that remains, the repository webhook on the fixture repo (5b), which only
// the dev-public edge can deliver.
//
// This test asserts the env-gate is present and structural. It does NOT execute
// the script — purely body inspection.
func TestSeedPostCleanroomAll_DevPublicStepsGatedByEnv(t *testing.T) {
	body := readScript(t, "seed-post-cleanroom-all.sh")

	gateRe := regexp.MustCompile(`\[\[\s*"\$\{?LOOPZERO_HELMFILE_ENV[:\-]*\}?"\s*==\s*"dev-public"\s*\]\]`)
	if !gateRe.MatchString(body) {
		t.Error("seed-post-cleanroom-all.sh does not contain the canonical env-gate `[[ \"$LOOPZERO_HELMFILE_ENV\" == \"dev-public\" ]]` — issue #99 fix missing")
	}

	if !stepIsInsideEnvGate(body, "seed github-webhook-fixture") {
		t.Error("Step 5b (github-webhook-fixture) is NOT inside the LOOPZERO_HELMFILE_ENV=dev-public conditional — issue #99 regression")
	}

	// Env-agnostic steps must stay unconditional — gating them would be an
	// over-scoped fix that silently stops seeding every other environment.
	for _, step := range []string{"seed minio", "seed github-app-dev"} {
		if stepIsInsideEnvGate(body, step) {
			t.Errorf("step %s is inside the env-gate but should remain unconditional — over-scoped fix", step)
		}
	}

	skipPathRe := regexp.MustCompile(`skip_step\s+"5b-[^"]+"\s+"[^"]*dev-public[^"]*"`)
	if !skipPathRe.MatchString(body) {
		t.Error("SKIP path for Step 5b does not log a dev-public rationale — operators won't understand the SKIP (issue #99 UX requirement)")
	}
}

// stepIsInsideEnvGate returns true when the run_step invocation for the
// given step script is between the `[[ "$LOOPZERO_HELMFILE_ENV" == "dev-public" ]]`
// condition and the matching `fi`. Uses a line-by-line scan tracking gate
// depth (gates do not nest in this script).
func stepIsInsideEnvGate(body, stepScript string) bool {
	lines := strings.Split(body, "\n")
	gateRe := regexp.MustCompile(`if\s+\[\[\s*"\$\{?LOOPZERO_HELMFILE_ENV[:\-]*\}?"\s*==\s*"dev-public"\s*\]\]`)
	inGate := false
	depth := 0
	for _, raw := range lines {
		trim := strings.TrimSpace(raw)
		if gateRe.MatchString(trim) {
			inGate = true
			depth = 1
			continue
		}
		if !inGate {
			continue
		}
		// Track shell `if`/`fi` depth inside the gate to handle nested
		// conditionals (e.g., the [[ -x ... ]] script-presence check that
		// already exists for Step 4).
		if strings.HasPrefix(trim, "if ") || trim == "if" {
			depth++
		}
		if trim == "fi" {
			depth--
			if depth == 0 {
				inGate = false
				continue
			}
		}
		if strings.Contains(trim, stepScript) {
			return true
		}
	}
	return false
}
