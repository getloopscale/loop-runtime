// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Regression-lock for the live-proof-dev-public.sh ENV_FILE canonical
// fix landed on 2026-06-20 during the canonical seed-layer close session.
//
// Symptom (caught LIVE during canonical close):
//
//   $ bash archive/canonical-tests-v1/scripts/local/live-proof-dev-public.sh --json
//   [live-proof env=dev-public] starting (...)
//   [live-proof env=dev-public] check=oidc
//   exit=2
//
// The script exited 2 silently after one line. Root cause: line 85 set
// `: "${ENV_FILE:=/home/paulo/loopzero/.env}"` — a HARDCODED ABSOLUTE PATH
// to a pre-M2-decomposition `.env` file that no longer exists. The
// `env_get` function grepped a non-existent file under `set -euo
// pipefail`; the failing assignment aborted the script before any
// check could run.
//
// Canonical 2026-06-20 finding: post-M2 decomposition the dev-public
// env SoT is `~/.loopzero/dev-credentials.json` (JSON), NOT a flat
// KEY=VALUE `.env` file. The sibling scripts already use this SoT:
//
//   - `infra/scripts/local/secrets-seed-dev.sh:26`
//       CREDS_FILE="${CREDS_FILE:-$HOME/.loopzero/dev-credentials.json}"
//   - `infra/scripts/local/setup-dev-public-dns.sh:36`
//       requires CLOUDFLARE_API_TOKEN exported from the JSON
//
// The fix migrates `live-proof-dev-public.sh` to the same canonical
// SoT: switch the default to `~/.loopzero/dev-credentials.json` and
// rewrite `env_get()` to derive each `LOOPZERO_*` key from the JSON via
// `jq` instead of grepping a KEY=VALUE file.
//
// SCOPE DISCIPLINE — read before extending:
//
//   This guard ONLY asserts that the dead `/home/paulo/loopzero/.env`
//   literal is GONE and the canonical JSON SoT path is present. It
//   does NOT validate every per-key jq mapping (that would couple this
//   hermetic test to the JSON schema; the script's runtime checks
//   surface schema drift naturally — a missing key returns "" and the
//   Layer-1 check bad()s honestly).
//
// The audit is encoded as a pure function so the negative-control test
// can mutate the in-memory body and PROVE the guard catches a revert.
// A guard that passes the negative control is theatre.
//
// Cross-references:
//   - Script: archive/canonical-tests-v1/scripts/local/live-proof-dev-public.sh
//   - Canonical JSON SoT: ~/.loopzero/dev-credentials.json (sibling scripts cite this)
//   - Sister guards: dogfood_all_repo_root_test.go,
//     connect_canonical_fixture_container_name_test.go
//   - Follow-up tracking: issue/task #107
package charts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Canonical literals — strings.Contains works fine; no regex needed.
const (
	deadEnvFileLiteral      = `/home/paulo/loopzero/.env`
	canonicalCredsSoTPath   = `dev-credentials.json`
	canonicalCredsRefHome   = `$HOME/.loopzero/dev-credentials.json`
)

// containsLiteralInCodeLine returns true when `needle` appears in any
// line of `body` whose first non-whitespace character is NOT `#` (i.e.,
// not a shell comment line). This lets the audit-trail comment
// legitimately reference historical literals (e.g., the dead
// `/home/paulo/loopzero/.env` path) without triggering the guard, while
// still catching the same literal if it slipped into a code line.
func containsLiteralInCodeLine(body, needle string) bool {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

func loadLiveProofDevPublic(t *testing.T) (body string, scriptPath string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	scriptPath = filepath.Join(root, "archive/canonical-tests-v1/scripts/local/live-proof-dev-public.sh")
	b, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", scriptPath, err)
	}
	return string(b), scriptPath
}

// auditLiveProofEnvFile encodes the #107 regression-lock contract.
// Returns failure messages (empty slice => guard passes).
func auditLiveProofEnvFile(body string) []string {
	var errs []string

	// (a) The dead `/home/paulo/loopzero/.env` literal MUST NOT appear in
	//     CODE LINES (comments are allowed to mention it as historical
	//     audit-trail context). This is the exact orphan shape from
	//     the 2026-06-20 catch.
	if containsLiteralInCodeLine(body, deadEnvFileLiteral) {
		errs = append(errs, "(a) script CONTAINS the dead literal `/home/paulo/loopzero/.env` in a CODE line — this absolute path was deleted in the M2 decomposition and no longer exists on disk. `env_get`'s grep against a missing file under `set -euo pipefail` aborts the script at exit 2 before any check runs. Migrate to `~/.loopzero/dev-credentials.json` per sibling scripts (secrets-seed-dev.sh:26).")
	}

	// (b) The canonical JSON SoT MUST be referenced — either via
	//     CREDS_FILE default OR via direct path. This proves the
	//     migration is in place, not just that the bad literal was
	//     deleted (e.g., commented out without replacement).
	hasCanonicalSoT := strings.Contains(body, canonicalCredsSoTPath)
	if !hasCanonicalSoT {
		errs = append(errs, "(b) script does NOT reference the canonical post-decomposition env SoT `dev-credentials.json`. The dead `.env` literal alone being absent is NOT enough — the canonical replacement source must be in place. Sibling canonical scripts: secrets-seed-dev.sh:26, setup-dev-public-dns.sh:36.")
	}

	// (c) Audit-trail comment cites the 2026-06-20 fix + the
	//     post-decomposition reasoning. Operators reading this in 6
	//     months must find the rationale without spelunking commits.
	hasDate := strings.Contains(body, "2026-06-20") || strings.Contains(body, "2026-06-")
	hasDecompReasoning := strings.Contains(body, "decomposition") ||
		strings.Contains(body, "M2") ||
		strings.Contains(body, "dev-credentials.json") ||
		strings.Contains(body, "#107")
	if !hasDate {
		errs = append(errs, "(c) audit-trail comment does NOT cite a 2026-06-* date — operators reading this in 6 months won't know when/why the env source was canonicalized.")
	}
	if !hasDecompReasoning {
		errs = append(errs, "(c) audit-trail comment does NOT reference the M2 decomposition / canonical JSON SoT reasoning — operators reading this in 6 months won't know why `~/.loopzero/dev-credentials.json` is the right source.")
	}

	return errs
}

// TestLiveProofEnvFileCanonical runs the #107 regression-lock guard
// against the canonical live-proof-dev-public.sh on disk. Expected
// verdict: GREEN after the 2026-06-20 canonical fix.
func TestLiveProofEnvFileCanonical(t *testing.T) {
	body, _ := loadLiveProofDevPublic(t)
	if errs := auditLiveProofEnvFile(body); len(errs) > 0 {
		for _, e := range errs {
			t.Error(e)
		}
	}
}

// TestLiveProofEnvFileCanonical_NegativeControl proves the guard would
// catch the #107 regression. It mutates the in-memory script body to
// re-introduce the dead `/home/paulo/loopzero/.env` literal AND removes
// the canonical SoT reference, asserts auditLiveProofEnvFile() reports
// at least one error. A guard that passes the negative control is
// theatre.
func TestLiveProofEnvFileCanonical_NegativeControl(t *testing.T) {
	body, _ := loadLiveProofDevPublic(t)

	// Re-introduce the dead literal (simulates a half-revert).
	mutated := body + "\n: \"${ENV_FILE:=/home/paulo/loopzero/.env}\"\n"

	errs := auditLiveProofEnvFile(mutated)
	if len(errs) == 0 {
		t.Errorf("negative control: auditLiveProofEnvFile() PASSED against a script body containing the dead `/home/paulo/loopzero/.env` literal — the guard would NOT catch the #107 regression (theatre).")
	}
	for _, e := range errs {
		t.Logf("negative-control verdict (expected to fire): %s", e)
	}

	// Belt-and-suspenders: also mutate the canonical SoT away and
	// verify check (b) fires independently.
	noSoT := strings.ReplaceAll(body, "dev-credentials.json", "REPLACED-WITHOUT-SOT")
	if !strings.Contains(noSoT, "REPLACED-WITHOUT-SOT") {
		// Pre-fix state: canonical SoT not yet present. The mutation is
		// a no-op; the negative-control sub-case is vacuous in that
		// case. The dead-literal sub-case above covers RED state.
		return
	}
	bErrs := auditLiveProofEnvFile(noSoT)
	bFired := false
	for _, e := range bErrs {
		if strings.Contains(e, "(b)") {
			bFired = true
			break
		}
	}
	if !bFired {
		t.Errorf("negative control sub-case: removing canonical SoT reference did NOT trigger check (b) — guard incomplete.")
	}
}
