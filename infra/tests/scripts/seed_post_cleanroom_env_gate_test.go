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
// fix for issue #99 (Cleanroom canonical chain — seed-post-cleanroom-all.sh
// not gating Step 4 on LOOPZERO_HELMFILE_ENV).
//
// Pre-fix behavior: the master seed script ran Step 4 (patch-kratos-dev-public)
// + Step 4b (seed-hydra-urls-canonical) unconditionally, regardless of the
// helmfile env. Both steps configure dev-public URLs (auth-dev.loopzero.example,
// app-dev.loopzero.example) which are NOT applicable in standalone-multi env
// (where Kratos + Hydra use localhost URLs via Tilt port-forward). Running
// the master seed in default standalone-multi env produced:
//
//   [patch-kratos-dev-public] ERROR: LOOPZERO_GITHUB_CLIENT_ID not set in env,
//     /home/paulo/loopzero/.env, or /home/paulo/.loopzero/dev-credentials.json
//
// + cascading FAILs in Steps 3 (admin), 9 (tenant-fixture), 11 (connect-
// fixture) because they depend on the dev-public-shaped Kratos config.
//
// Canonical fix: wrap Steps 4 + 4b in a conditional that requires
// LOOPZERO_HELMFILE_ENV=dev-public. In other envs (standalone-multi, drill, prod)
// the steps are SKIPPED with an explanatory log message, NOT failed.
//
// This test asserts the env-gate is present, structural, and traceable. It
// does NOT execute the script — purely body inspection.
func TestSeedPostCleanroomAll_DevPublicStepsGatedByEnv(t *testing.T) {
	body := readScript(t, "seed-post-cleanroom-all.sh")

	// T1. The script MUST reference LOOPZERO_HELMFILE_ENV at least once.
	if !strings.Contains(body, "LOOPZERO_HELMFILE_ENV") {
		t.Error("seed-post-cleanroom-all.sh has no LOOPZERO_HELMFILE_ENV reference — env-gating not implemented (issue #99 regression)")
	}

	// T2. The Step 4 (patch-kratos-dev-public) invocation MUST be inside a
	// conditional that requires env=dev-public. Detection heuristic:
	// the bash `run_step ... patch-kratos-dev-public.sh` line MUST be
	// preceded within a small window by an `if [[ ... "dev-public" ... ]]`
	// test. We search for the run_step line then walk backward up to 8
	// non-blank lines looking for the gate condition.
	gateRe := regexp.MustCompile(`\[\[\s*"\$\{?LOOPZERO_HELMFILE_ENV[:\-]*\}?"\s*==\s*"dev-public"\s*\]\]`)
	if !gateRe.MatchString(body) {
		t.Error("seed-post-cleanroom-all.sh does not contain the canonical env-gate `[[ \"$LOOPZERO_HELMFILE_ENV\" == \"dev-public\" ]]` — issue #99 fix missing")
	}

	// T3. Step 4 (patch-kratos-dev-public) run_step line MUST be inside
	// the env-gated block. We detect this by checking that between the
	// gate condition and the run_step line, there are no `else` or `fi`
	// tokens that would close the block prematurely.
	// Migrated to Go seed 2026-07-08: the invocation is now `seed patch-kratos`.
	if !stepIsInsideEnvGate(body, "seed patch-kratos") {
		t.Error("Step 4 (patch-kratos) is NOT inside the LOOPZERO_HELMFILE_ENV=dev-public conditional — issue #99 regression")
	}

	// T4. Step 4b (seed-hydra-urls-canonical) MUST be inside the same
	// env-gate block (or its own equivalent gate). Hydra URLs are
	// equally dev-public-specific.
	if !stepIsInsideEnvGate(body, "seed hydra-urls") {
		t.Error("Step 4b (seed-hydra-urls-canonical) is NOT inside the LOOPZERO_HELMFILE_ENV=dev-public conditional — issue #99 regression")
	}

	// T5. Other steps (1 vault, 2 minio, 5 github-app, 6 gitea-token, 7
	// broker, 8 loki, 9 tenant-fixture, 10 admin-hydra-client, 11 connect-
	// fixture) MUST remain unconditional (or have their own independent
	// gating logic, but NOT the dev-public env gate). Spot-check Steps 2
	// and 5 — both env-agnostic.
	// M15 Wave 1: seed-github-app-dev.sh → `loopzero-ops seed github-app-dev`.
	for _, step := range []string{"seed minio", "seed github-app-dev"} {
		if stepIsInsideEnvGate(body, step) {
			t.Errorf("step %s is inside the env-gate but should remain unconditional — over-scoped fix", step)
		}
	}

	// T6. The SKIP path MUST emit a clear message that operators see when
	// running in non-dev-public env. Detect via skip_step invocation
	// referencing "dev-public" reason.
	skipPathRe := regexp.MustCompile(`skip_step\s+"4-patch-kratos-dev-public"\s+"[^"]*dev-public[^"]*"`)
	if !skipPathRe.MatchString(body) {
		t.Error("SKIP path for Step 4 does not log a dev-public rationale — operators won't understand the SKIP (issue #99 UX requirement)")
	}

	// T7. Traceability comment cites issue #99 so a future "simplify"
	// refactor that removes the env-gate sees the rationale.
	if !strings.Contains(body, "issue #99") && !strings.Contains(body, "Finding #99") {
		t.Error("Issue #99 traceability comment missing — future refactor may remove the env-gate without understanding why")
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
