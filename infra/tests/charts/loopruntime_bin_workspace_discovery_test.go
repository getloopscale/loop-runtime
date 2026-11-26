// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Regression-lock for the connect-canonical-fixture.sh LOOPRUNTIME_BIN
// workspace-discovery fix landed on 2026-06-20 (issue #108).
//
// Symptom: the master orchestrator `seed-post-cleanroom-all.sh` invokes
// Step 11 `connect-canonical-fixture.sh` without exporting LOOPRUNTIME_BIN.
// Pre-fix, Step 11 emitted WARN+return-2 because the script defaulted
// to `LOOPRUNTIME_BIN="${LOOPRUNTIME_BIN:-loopruntime}"` and `command -v loopruntime` failed when
// `loopruntime` wasn't on the operator's PATH. The canonical LoopRuntime CLI lives
// in a SIBLING repo post-M2 decomposition: `loopruntime-cli/bin/loopruntime` at the
// workspace root.
//
// Fix: 1-line derivation that prefers (a) the operator-exported
// LOOPRUNTIME_BIN, then (b) `loopruntime` on PATH (works on dev machines where
// `loopruntime-cli/` was npm-linked), then (c) workspace-relative discovery
// of `loopruntime-cli/bin/loopruntime` (the canonical post-decomp location). Falls
// back to literal `"loopruntime"` so the existing WARN path remains the
// last-resort signal when nothing resolves.
//
// SCOPE DISCIPLINE — read before extending:
//
//	This guard ONLY asserts the workspace-relative discovery path is
//	present + the older `LOOPRUNTIME_BIN="${LOOPRUNTIME_BIN:-loopruntime}"` literal is GONE
//	from code lines. It does NOT validate that the workspace topology
//	itself is correct (the script's runtime `command -v` check is
//	the canonical signal when discovery fails).
//
// Cross-references:
//   - Script: infra/scripts/local/connect-canonical-fixture.sh
//   - Workspace canonical CLI: loopruntime-cli/bin/loopruntime (sibling repo)
//   - Sister guards: dogfood_all_repo_root_test.go,
//     live_proof_env_file_test.go,
//     connect_canonical_fixture_container_name_test.go
//   - Follow-up tracking: issue/task #108
package charts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadConnectFixtureForLoopRuntimeBin(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	p := filepath.Join(root, "infra/scripts/local/connect-canonical-fixture.sh")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// auditLoopRuntimeBinWorkspaceDiscovery encodes the #108 regression-lock contract.
// Returns failure messages (empty slice => guard passes).
func auditLoopRuntimeBinWorkspaceDiscovery(body string) []string {
	var errs []string

	// (a) The workspace-canonical CLI path `loopruntime-cli/bin/loopruntime` MUST be
	//     referenced in the script's LOOPRUNTIME_BIN derivation. Otherwise the
	//     script defaults to `loopruntime` on PATH and Step 11 fails when
	//     called from `seed-post-cleanroom-all.sh` without LOOPRUNTIME_BIN
	//     pre-exported.
	if !strings.Contains(body, "loopruntime-cli/bin/loopruntime") {
		errs = append(errs, "(a) script does NOT reference workspace-canonical `loopruntime-cli/bin/loopruntime` — post-M2 decomposition the LoopRuntime CLI lives in the sibling loopruntime-cli repo. Without workspace-relative discovery, `seed-post-cleanroom-all.sh` Step 11 emits WARN+return-2 when called without LOOPRUNTIME_BIN pre-exported.")
	}

	// (b) The older bare `LOOPRUNTIME_BIN="${LOOPRUNTIME_BIN:-loopruntime}"` literal MUST
	//     NOT appear in CODE lines (it's fine in comment audit-trail).
	//     The new derivation chains discovery; the old literal alone
	//     would re-introduce the bug.
	if containsLiteralInCodeLine(body, `LOOPRUNTIME_BIN="${LOOPRUNTIME_BIN:-loopruntime}"`) {
		errs = append(errs, "(b) script CONTAINS the pre-fix `LOOPRUNTIME_BIN=\"${LOOPRUNTIME_BIN:-loopruntime}\"` literal in a CODE line — this defaults to PATH lookup without any workspace-relative discovery. Replace with the chained discovery (operator-exported → PATH → workspace-relative → fallback).")
	}

	// (c) Audit-trail comment cites the 2026-06-20 fix + post-decomp
	//     reasoning.
	hasDate := strings.Contains(body, "2026-06-20") || strings.Contains(body, "2026-06-")
	hasDecomp := strings.Contains(body, "decomposition") ||
		strings.Contains(body, "loopruntime-cli/bin/loopruntime") ||
		strings.Contains(body, "#108")
	if !hasDate {
		errs = append(errs, "(c) audit-trail does NOT cite a 2026-06-* date for the workspace-discovery fix — operators reading this in 6 months won't know when/why.")
	}
	if !hasDecomp {
		errs = append(errs, "(c) audit-trail does NOT cite the post-M2 decomposition reasoning — operators won't know why workspace-relative discovery exists.")
	}

	return errs
}

// TestLoopRuntimeBinWorkspaceDiscovery runs the #108 regression-lock guard.
func TestLoopRuntimeBinWorkspaceDiscovery(t *testing.T) {
	body := loadConnectFixtureForLoopRuntimeBin(t)
	if errs := auditLoopRuntimeBinWorkspaceDiscovery(body); len(errs) > 0 {
		for _, e := range errs {
			t.Error(e)
		}
	}
}

// TestLoopRuntimeBinWorkspaceDiscovery_NegativeControl proves the guard would
// catch the #108 regression by re-introducing the pre-fix literal.
func TestLoopRuntimeBinWorkspaceDiscovery_NegativeControl(t *testing.T) {
	body := loadConnectFixtureForLoopRuntimeBin(t)

	// Re-introduce the dead literal as a code line.
	mutated := body + "\nLOOPRUNTIME_BIN=\"${LOOPRUNTIME_BIN:-loopruntime}\"\n"

	errs := auditLoopRuntimeBinWorkspaceDiscovery(mutated)
	if len(errs) == 0 {
		t.Errorf("negative control: auditLoopRuntimeBinWorkspaceDiscovery() PASSED against a script body containing the pre-fix literal — the guard would NOT catch the #108 regression (theatre).")
	}
	for _, e := range errs {
		t.Logf("negative-control verdict (expected to fire): %s", e)
	}
}
