// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package placement

import (
	"strings"
	"testing"
)

// The hint emitted when the database moved and the CR patch failed must say the
// direct patch is DENIED by the admission policy. The previous hint offered a
// bare `kubectl patch`, and since 2026-08-13 `loopruntime-tenant-cr-shape` denies
// exactly that change — whoever followed it with an unprivileged kubeconfig got
// `Denied` with no reason (B-097).
func TestRepairHint_WarnsThatTheDirectPatchIsDenied(t *testing.T) {
	hint := repairHint("loopruntime-system", "tn-abc", "cell-1")

	for _, required := range []string{
		"DENIED",                        // the central fact
		"loopruntime-tenant-cr-shape",   // which policy
		"system:masters",                // who is exempt
		"kubectl -n loopruntime-system", // the command stays quoted, for whoever IS exempt
		"tn-abc",
		"cell-1",
	} {
		if !strings.Contains(hint, required) {
			t.Errorf("the hint does not mention %q:\n%s", required, hint)
		}
	}
}

// The obvious and wrong advice: this function IS the `loopruntime admin tenants pin`
// path, and it just failed. Telling the operator to use it would repeat what did
// not work without saying what changed.
func TestRepairHint_DoesNotPointAtTheCommandThatJustFailed(t *testing.T) {
	hint := repairHint("loopruntime-system", "tn-abc", "cell-1")
	if strings.Contains(hint, "loopruntime admin tenants pin") {
		t.Errorf("the hint points at the command whose own path just failed:\n%s", hint)
	}
	if !strings.Contains(hint, "Re-run the pin") {
		t.Errorf("the hint does not say what actually unblocks it — re-running so the patch comes from the exempt identity:\n%s", hint)
	}
}
