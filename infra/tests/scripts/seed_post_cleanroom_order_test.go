// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"regexp"
	"testing"
)

// scriptPath + readScript helpers live in vault_bootstrap_test.go
// (shared package-level helpers, landed first via PR #92).

// TestSeedPostCleanroomAll_StepOrderCanonical locks the dependency order of
// the master seed chain.
//
// Finding #4 of issue #91 (Cleanroom mandate chain run 2026-06-18) was an order
// defect: Step 3 (seed-admin-loop-runtime-dev) POSTs a login against auth-dev, and
// ran before the patch-kratos seed had rewritten the Kratos URLs, so every fresh
// Cleanroom failed the admin login. The fix of record was to run patch-kratos
// first. The root cause is gone now — the Kratos and Hydra URLs are environment
// values `helmfile sync` renders before any seed runs (principle_violations#67)
// — so the relation left to lock is the one that still exists.
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

	// No step may seed the Kratos or Hydra configuration again: that is what
	// made the order matter in the first place.
	for _, id := range []string{"4", "4b"} {
		if _, back := positions[id]; back {
			t.Errorf("step %q is back in the chain; the Kratos/Hydra URLs are rendered by helmfile, and a seed "+
				"patching them re-creates the Finding #4 ordering dependency", id)
		}
	}
}
