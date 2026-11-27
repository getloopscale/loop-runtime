// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"regexp"
	"strings"
	"testing"
)

// scriptPath + readScript helpers live in vault_bootstrap_test.go
// (shared package-level helpers, landed first via PR #92).

// TestSeedPostCleanroomAll_RunStepAlwaysReturnsZero locks Finding #5 of
// issue #91 (Cleanroom mandate canonical chain run 2026-06-18).
//
// The legacy run_step function returned 1 on failure. Combined with the
// top-level `set -euo pipefail`, the FIRST failing step aborted the
// entire script — meaning:
//
//  1. The "=== canonical chain summary ===" log section (lines ~162-178)
//     was UNREACHABLE on any failure. Operators saw a wrapper exit code
//     of 0 (because the only context tracking the chain was the
//     external `&&` operator in the wrapper) but had no summary of which
//     steps actually ran, which passed, and which failed.
//
//  2. Downstream steps that DID NOT depend on the failing step were
//     never attempted. E.g., if Step 2 (minio) timed out because
//     Step 1 vault wrote the wrong Secret namespace (Finding #2), the
//     operator could not see that Step 5 (github-app) and Step 6
//     (gitea-token) would have passed independently. Triage required
//     re-running the script manually after each fix.
//
// Canonical fix: run_step accumulates FAIL count + always returns 0 so
// the chain proceeds. The final exit code (line ~180) is non-zero if
// any FAIL accumulated — preserving the "fail loud at end" contract
// without sacrificing observability.
//
// Test asserts:
//   - run_step has NO `return 1` statement.
//   - run_step ends with `return 0` (or no return — implicit 0).
//   - The summary log block runs unconditionally (no guard).
//   - Final `[[ "$FAIL" == "0" ]] || exit 1` exits non-zero if any
//     failure accumulated.
//   - Traceability comment present (Finding #5 / issue #91).
func TestSeedPostCleanroomAll_RunStepAlwaysReturnsZero(t *testing.T) {
	body := readScript(t, "seed-post-cleanroom-all.sh")

	// Extract the run_step function body. The function is defined at
	// top level with the canonical `run_step() {` opening.
	runStepRe := regexp.MustCompile(`(?s)run_step\(\) \{(.*?)\n\}`)
	m := runStepRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("run_step function not found in script — structure changed")
	}
	runStepBody := m[1]

	// T1. run_step MUST NOT contain `return 1` — that propagates up to
	// `set -e` and aborts the chain. Internal FAIL accumulation handles
	// the verdict at the end of the script instead.
	if strings.Contains(runStepBody, "return 1") {
		t.Error("run_step contains 'return 1' — failure propagates via set -e and skips the summary. Use FAIL counter + return 0 instead (Finding #5).")
	}

	// T2. The function MUST end with `return 0` (explicit) OR have no
	// return inside the failure branch (implicit 0). Defensive: catches
	// a refactor that re-introduces an early `return $rc`.
	hasReturnZero := strings.Contains(runStepBody, "return 0")
	if !hasReturnZero {
		t.Error("run_step has no `return 0` — implicit exit on last statement may regress to non-zero. Add an explicit terminal return 0 (Finding #5).")
	}

	// T3. The summary log block runs unconditionally — i.e., the
	// "=== canonical chain summary ===" header appears OUTSIDE any
	// conditional / function. Searching for the literal header and
	// ensuring it is NOT nested inside `if`/`elif`/function braces
	// requires a structural check; a simpler proxy: the header line
	// has no leading whitespace beyond what the `log` call itself
	// produces. Existing line: `log "=== canonical chain summary ==="`
	// at column 0.
	summaryRe := regexp.MustCompile(`(?m)^log "=== canonical chain summary ===`)
	if !summaryRe.MatchString(body) {
		t.Error("canonical chain summary log block missing at top level — Finding #5 regression")
	}

	// T4. Final fail-loud exit at end of script: the FAIL counter MUST
	// drive a non-zero exit if any step failed. Pattern:
	// `[[ "$FAIL" == "0" ]] || exit 1`.
	finalExitRe := regexp.MustCompile(`\[\[\s*"\$FAIL"\s*==\s*"0"\s*\]\]\s*\|\|\s*exit\s+1`)
	if !finalExitRe.MatchString(body) {
		t.Error("final FAIL-driven exit not found — script may report VERDICT FAIL but still exit 0")
	}

	// T5. Traceability comment cites Finding #5 / issue #91 in or near
	// the run_step function so a future "simplify" refactor that re-adds
	// `return 1` sees the rationale.
	if !strings.Contains(body, "Finding #5") && !strings.Contains(body, "Finding #5 of issue #91") {
		t.Error("Finding #5 traceability comment missing — future refactor may re-introduce the silent-fail behavior")
	}
}
