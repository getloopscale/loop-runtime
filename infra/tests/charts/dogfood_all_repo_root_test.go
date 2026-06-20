// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Regression-lock for the dogfood-all.sh REPO_ROOT canonical fix landed
// on 2026-06-20 during the dev-public Cleanroom-fresh canonical close.
//
// Symptom (caught LIVE during seed canonical close session):
//
//   $ bash archive/canonical-tests-v1/scripts/local/dogfood-all.sh --live-only --json
//   {"pass":0,"fail":0,"skip":13,"total":13,"verdict":"PASS",
//    "results":["SKIP: dogfood-recovery (script missing on disk)", ...]}
//
// Root cause: the script was relocated from `loopzero/infra/scripts/local/`
// (3 levels deep) to `loopzero/archive/canonical-tests-v1/scripts/local/`
// (4 levels deep) during M2 decomposition. The REPO_ROOT derivation
// `$(cd "$(dirname "$0")/../../.." && pwd)` was NOT updated — it still
// climbs only 3 levels, landing at `loopzero/archive/` instead of `loopzero/`.
// The WRAPPERS array (lines 85-97) lists relative paths
// `archive/canonical-tests-v1/scripts/local/dogfood-{journey}.sh` that
// resolve against REPO_ROOT — so `loopzero/archive/` + `archive/...` =
// `loopzero/archive/archive/...` → DOES NOT EXIST → 13/13 SKIP.
//
// Fix: one extra `..` in the cd path so REPO_ROOT climbs 4 levels
// instead of 3: `$(dirname "$0")/../../../..`.
//
// Path-arithmetic audit:
//   $(dirname dogfood-all.sh) = loopzero/archive/canonical-tests-v1/scripts/local
//   ../ = loopzero/archive/canonical-tests-v1/scripts
//   ../../ = loopzero/archive/canonical-tests-v1
//   ../../../ = loopzero/archive
//   ../../../../ = loopzero  ← canonical REPO_ROOT
//
// SCOPE DISCIPLINE — read before extending:
//
//   This guard ONLY asserts the REPO_ROOT path arithmetic + that the
//   first wrapper resolves on disk. It does NOT validate each wrapper's
//   exit code (that depends on LIVE cluster state). The success
//   criterion is "no longer SKIP-missing"; individual wrapper PASS/FAIL
//   is a separate concern.
//
// The audit is encoded as a pure function so the negative-control test
// can mutate the in-memory body and PROVE the guard catches a revert.
// A guard that passes the negative control is theatre.
//
// Cross-references:
//   - Script: archive/canonical-tests-v1/scripts/local/dogfood-all.sh
//   - Sister guards: connect_canonical_fixture_container_name_test.go,
//     vault_bootstrap_san_aligned_host_test.go
//   - Mandate step 4 (DOGFOOD FULL): .claude/rules/cleanroom-up-live-test-mandate.md
//   - Follow-up tracking: issue/task #106
package charts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Regex inventory — raw strings preserve on-disk shape.
//
// The canonical line shipped 2026-06-20 is:
//
//   REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
//
// The pre-fix buggy line was:
//
//   REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
var (
	reRepoRootCanonical = regexp.MustCompile(`REPO_ROOT="\$\(cd "\$\(dirname "\$0"\)/\.\./\.\./\.\./\.\." && pwd\)"`)
	reRepoRootBuggy     = regexp.MustCompile(`REPO_ROOT="\$\(cd "\$\(dirname "\$0"\)/\.\./\.\./\.\." && pwd\)"`)
	reFirstWrapper      = regexp.MustCompile(`"archive/canonical-tests-v1/scripts/local/dogfood-recovery\.sh"`)
)

func loadDogfoodAll(t *testing.T) (body string, scriptPath string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	scriptPath = filepath.Join(root, "archive/canonical-tests-v1/scripts/local/dogfood-all.sh")
	b, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", scriptPath, err)
	}
	return string(b), scriptPath
}

// auditDogfoodAllRepoRoot encodes the #106 regression-lock contract.
// Returns failure messages (empty slice => guard passes). Pulling the
// assertions into a pure function lets the negative-control test mutate
// the body and prove the guard catches the regression.
func auditDogfoodAllRepoRoot(body string) []string {
	var errs []string

	// (a) The canonical 4-dotdot REPO_ROOT derivation MUST be present.
	if !reRepoRootCanonical.MatchString(body) {
		errs = append(errs, "(a) script does NOT contain the canonical `REPO_ROOT=\"$(cd \"$(dirname \"$0\")/../../../..\" && pwd)\"` (4 `..` segments). The script lives 4 levels deep at `loopzero/archive/canonical-tests-v1/scripts/local/`; the REPO_ROOT must climb 4 levels to land at `loopzero/`, the root the WRAPPERS array's paths are relative to.")
	}

	// (b) The buggy 3-dotdot derivation MUST NOT appear — this is the
	//     exact bug shape from the 2026-06-20 catch.
	if reRepoRootBuggy.MatchString(body) {
		errs = append(errs, "(b) script CONTAINS the buggy `REPO_ROOT=\"$(cd \"$(dirname \"$0\")/../../..\" && pwd)\"` (3 `..` segments). With the script at `loopzero/archive/canonical-tests-v1/scripts/local/`, 3 climbs land at `loopzero/archive/`, NOT `loopzero/`. The WRAPPERS paths `archive/canonical-tests-v1/scripts/local/dogfood-*.sh` then resolve to `loopzero/archive/archive/...` → all 13 SKIP with `script missing on disk`.")
	}

	// (c) The WRAPPERS array MUST still reference the canonical
	//     archive-relative first wrapper path. If someone moved the
	//     scripts out of `archive/` the REPO_ROOT arithmetic would need
	//     a different number of climbs, invalidating this guard's logic.
	if !reFirstWrapper.MatchString(body) {
		errs = append(errs, "(c) WRAPPERS array does NOT contain `\"archive/canonical-tests-v1/scripts/local/dogfood-recovery.sh\"` — the script tree shape changed (e.g., scripts moved out of `archive/`). REPO_ROOT arithmetic in this guard ASSUMES the archive/canonical-tests-v1/scripts/local depth; re-derive both before extending.")
	}

	// (d) Audit-trail comment cites the 2026-06-20 canonical close +
	//     the relocation reasoning. Operators reading this in 6 months
	//     must find the rationale without spelunking commits.
	hasRelocation := strings.Contains(body, "archive") &&
		(strings.Contains(body, "relocation") ||
			strings.Contains(body, "REPO_ROOT") ||
			strings.Contains(body, "moved"))
	if !hasRelocation {
		_ = hasRelocation // soft-check; not required to fail
	}

	return errs
}

// resolveDogfoodAllRepoRoot replicates the script's REPO_ROOT logic in
// Go so we can prove (on the host filesystem) that the 4-dotdot path
// lands at loopzero/ AND the 3-dotdot legacy path does NOT resolve the first
// wrapper. This is the empirical half of the guard.
func resolveDogfoodAllRepoRoot(scriptPath string, climbCount int) (string, bool) {
	dir := filepath.Dir(scriptPath)
	for i := 0; i < climbCount; i++ {
		dir = filepath.Dir(dir)
	}
	return dir, true
}

// TestDogfoodAllRepoRootCanonical runs the #106 regression-lock guard
// against the canonical dogfood-all.sh on disk. Expected verdict: GREEN
// after the 2026-06-20 canonical fix.
func TestDogfoodAllRepoRootCanonical(t *testing.T) {
	body, scriptPath := loadDogfoodAll(t)
	if errs := auditDogfoodAllRepoRoot(body); len(errs) > 0 {
		for _, e := range errs {
			t.Error(e)
		}
	}

	// Empirical half: prove the canonical 4-climb derivation resolves
	// the first wrapper on disk.
	root, _ := resolveDogfoodAllRepoRoot(scriptPath, 4)
	firstWrapper := filepath.Join(root, "archive/canonical-tests-v1/scripts/local/dogfood-recovery.sh")
	if _, err := os.Stat(firstWrapper); err != nil {
		t.Errorf("canonical 4-climb REPO_ROOT does NOT resolve the first wrapper on disk: %v — REPO_ROOT computed as %q, expected wrapper at %q. Either the relocation depth changed or the wrapper was moved.", err, root, firstWrapper)
	}
}

// TestDogfoodAllRepoRootCanonical_NegativeControl proves the guard
// would catch the #106 regression at the script-body level AND that the
// pre-fix 3-climb derivation does NOT resolve the first wrapper on
// disk (the empirical complement). A guard that passes the negative
// control is theatre.
func TestDogfoodAllRepoRootCanonical_NegativeControl(t *testing.T) {
	body, scriptPath := loadDogfoodAll(t)

	// Script-body half: mutate canonical → buggy + assert guard fires.
	// strings.Replace (NOT regex ReplaceAllString) — Go's regex Expand
	// semantics interpret `$0` in the replacement as the whole match,
	// which would garble the literal bash `$0` in our replacement string.
	canonicalLiteral := `REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"`
	buggyLiteral := `REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"`
	mutated := strings.Replace(body, canonicalLiteral, buggyLiteral, 1)
	if mutated == body {
		t.Fatalf("negative-control setup failed: could not find the canonical 4-dotdot REPO_ROOT literal in script body — either the script changed shape or the literal drifted. The GREEN test is vacuous.")
	}
	errs := auditDogfoodAllRepoRoot(mutated)
	if len(errs) == 0 {
		t.Errorf("negative control: auditDogfoodAllRepoRoot() PASSED against a script whose REPO_ROOT was reverted to 3 climbs — the guard would NOT catch the off-by-one (theatre).")
	}
	for _, e := range errs {
		t.Logf("negative-control verdict (expected to fire): %s", e)
	}

	// Empirical half: prove the 3-climb derivation does NOT resolve the
	// first wrapper on disk (would-be SKIP-missing).
	wrongRoot, _ := resolveDogfoodAllRepoRoot(scriptPath, 3)
	firstWrapperFromWrong := filepath.Join(wrongRoot, "archive/canonical-tests-v1/scripts/local/dogfood-recovery.sh")
	if _, err := os.Stat(firstWrapperFromWrong); err == nil {
		t.Errorf("negative control: the buggy 3-climb derivation %q + WRAPPERS path resolved a real wrapper file at %q — that would mean the off-by-one is NOT a regression (e.g., scripts present at both archive/archive/ AND archive/). Either the script tree was duplicated or the test is wrong.", wrongRoot, firstWrapperFromWrong)
	}
}
