// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"regexp"
	"testing"
)

// scriptPath + readScript helpers live in vault_bootstrap_test.go
// (shared package-level helpers, landed first via PR #92).

// TestSeedPostCleanroomAll_StepOrderCanonical locks Finding #4 of issue
// #91 (Cleanroom mandate canonical chain run 2026-06-18). The master
// seed script declared step ORDER:
//
//	1 vault → 2 minio → 3 admin → 4 patch-kratos → 4b hydra → 5+ ...
//
// but Step 3 (seed-admin-loopzero-dev) HTTP-POSTs a login submit against
// the canonical Kratos URL (auth-dev.loopzero.example). At Step 3 time,
// Kratos ConfigMap still has the helm-default URLs (localhost:4455)
// because Step 4 (patch-kratos-dev-public) has not run yet → CSRF
// defense rejects the submit with HTTP 000. Sponsor LIVE-caught
// 2026-06-18: every fresh Cleanroom failed Step 3 admin login.
//
// Canonical order: 1 vault → 2 minio → 4 patch-kratos → 4b hydra → 3
// admin → 5+. The login-submit dependency MUST come after Kratos
// canonical URLs land in the ConfigMap.
//
// Cross-ref memory feedback_post_cleanroom_kratos_patch_dev_public
// already documents the dependency; this test locks the ORDER so the
// master script does not regress.
func TestSeedPostCleanroomAll_StepOrderCanonical(t *testing.T) {
	body := readScript(t, "seed-post-cleanroom-all.sh")

	// Find the position (first byte) of each step's run_step / skip_step
	// invocation. The master script invokes each step exactly once via
	// `run_step "<id>-<name>"` (or `skip_step` when optional/absent),
	// which gives us a single canonical offset.
	stepRe := regexp.MustCompile(
		`(?m)^\s*(?:run_step|skip_step)\s+"([0-9]+[a-z]?)-([^"]+)"`,
	)
	matches := stepRe.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		t.Fatalf("no run_step / skip_step invocations found — script structure changed")
	}

	// Build id → first-match-position map (lowest position wins; later
	// re-invocations of the same step are tolerated for back-compat).
	positions := map[string]int{}
	order := []string{}
	for _, m := range matches {
		idStart, idEnd := m[2], m[3]
		id := body[idStart:idEnd]
		if _, seen := positions[id]; seen {
			continue
		}
		positions[id] = m[0]
		order = append(order, id)
	}

	// Mandatory ordering relations. The script may skip optional steps
	// (Step 10 admin-hydra-client / Step 11 connect-canonical-fixture are
	// gated behind `[[ -x "$SCRIPT_DIR/..." ]]`) so we only assert on
	// steps that MUST be present.
	requiredBefore := []struct {
		earlier, later string
		why            string
	}{
		{"1", "2", "vault must finish before minio (Kratos uses minio for cloud assets)"},
		// Canonical fix for Finding #4 — patch-kratos (4) MUST run
		// before admin (3) because admin posts a login submit against
		// auth-dev.loopzero.example and Kratos rejects pre-patch with HTTP
		// 000 (cors / cookies.domain / base_url still helm-default).
		{"4", "3", "patch-kratos MUST run before admin login submit (Finding #4 / issue #91)"},
		// Same family: hydra URLs canonical must also precede admin so
		// the OIDC callback chain is consistent.
		{"4b", "3", "hydra URLs canonical must run before admin (consistency with 4 → 3)"},
	}

	for _, r := range requiredBefore {
		earlierPos, hasEarlier := positions[r.earlier]
		laterPos, hasLater := positions[r.later]
		if !hasEarlier {
			t.Errorf("step %q not found in script (required before %q)", r.earlier, r.later)
			continue
		}
		if !hasLater {
			t.Errorf("step %q not found in script (required after %q)", r.later, r.earlier)
			continue
		}
		if earlierPos >= laterPos {
			t.Errorf("ORDER violation: step %q must run BEFORE step %q (%s); current order: %v",
				r.earlier, r.later, r.why, order)
		}
	}

	// Traceability comment: Finding #4 / issue #91 referenced in script
	// header so a future operator understands why the unusual order
	// (Step 4 before Step 3) is canonical.
	traceRe := regexp.MustCompile(`Finding #4|issue #91|patch-kratos.*before.*admin|MUST.*precede.*admin`)
	if !traceRe.MatchString(body) {
		t.Error("canonical traceability comment missing — future operator may 'fix' the unusual 4-before-3 order")
	}
}
