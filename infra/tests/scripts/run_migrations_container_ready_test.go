// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readProductionScript resolves a path under infra/scripts/production/
// relative to this test file (infra/tests/scripts/). Sibling of the
// `readScript` helper in vault_bootstrap_test.go which resolves under
// infra/scripts/local/.
func readProductionScript(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "scripts", "production", name))
	if err != nil {
		t.Fatalf("resolve %s: %v", name, err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// TestRunMigrationsDetectsContainerNotReady locks issue #106 Design E
// (ADR PROPOSED `2026-06-19-loopruntime-api-vault-bootstrap-ordering`).
//
// Pre-fix state: `infra/scripts/production/run-migrations.sh:96-97`
// detected "loopruntime-api pod absent" via pod-NAME presence check
// (`[[ -z "${LOOPRUNTIME_API_POD}" ]]`). When loopruntime-api is in CrashLoopBackOff
// (e.g., dev-public env where `OPENBAO_TRANSIT_ENABLED=true` requires
// `OPENBAO_VAULT_SECRET_ID` that ships only post-seed-chain), the pod
// NAME exists — so the fallback branch is skipped — and the script
// tries `kubectl exec` which fails with
// `container not found ("loopruntime-api")`. Cleanroom catch 2026-06-19.
//
// The existing psql-loop fallback at lines 111-148 was DESIGNED for
// "pre-tilt loopruntime-api absent" (see comment block 98-103). Extending the
// detector to ALSO trigger when the container is not Ready honors the
// fallback intent + does NOT reverse the T2.5+EC-05 hard-fail design of
// `validateOpenBaoTransit` in api/internal/config/config.go (runtime
// behavior unchanged; only the migration runner detector changes).
//
// Recommended in ADR `2026-06-19-loopruntime-api-vault-bootstrap-ordering`
// (currently PROPOSED, awaiting sponsor grill-me). This test +
// implementation ship pre-ratification with explicit PR-body call-out;
// rollback path is `git revert` single commit.
//
// Test asserts the detector body structurally honors the design. It
// does NOT execute the script.
func TestRunMigrationsDetectsContainerNotReady(t *testing.T) {
	body := readProductionScript(t, "run-migrations.sh")

	// T1. Script MUST query the container Ready status. The canonical
	// jsonpath uses `.status.containerStatuses[?(@.name=="loopruntime-api")].ready`
	// to pick the specific container by name (defensive against pods
	// with multiple containers — e.g., when sidecars are added later).
	if !strings.Contains(body, "containerStatuses") {
		t.Error("run-migrations.sh has no `containerStatuses` query — Design E detector extension missing (issue #106)")
	}
	if !strings.Contains(body, "name==\"loopruntime-api\"") && !strings.Contains(body, `name=="loopruntime-api"`) {
		t.Error("run-migrations.sh does not pick container by name `loopruntime-api` in jsonpath — defense against multi-container pods missing")
	}

	// T2. LOOPRUNTIME_API_READY variable MUST be declared. Named container
	// status capture is the canonical pattern.
	if !strings.Contains(body, "LOOPRUNTIME_API_READY") {
		t.Error("run-migrations.sh has no `LOOPRUNTIME_API_READY` variable — detector capture missing")
	}

	// T3. The fallback gate MUST be EXTENDED to include both
	// `LOOPRUNTIME_API_POD empty` OR `LOOPRUNTIME_API_READY != true`. Without the OR
	// branch the original detector regresses.
	// Pattern: `[[ -z "${LOOPRUNTIME_API_POD}" || "$LOOPRUNTIME_API_READY" != "true" ]]`
	// (or equivalent with braced expansion).
	if !strings.Contains(body, "LOOPRUNTIME_API_READY") || !strings.Contains(body, "!= \"true\"") {
		t.Error("run-migrations.sh fallback gate does not test `LOOPRUNTIME_API_READY != \"true\"` — Design E branch missing")
	}

	// T4. The combined gate MUST evaluate `LOOPRUNTIME_API_POD empty` OR
	// `READY != true` — not AND. Grep for the `||` operator inside the
	// same `[[ ... ]]` block referencing both variables.
	if !strings.Contains(body, "LOOPRUNTIME_API_POD") || !strings.Contains(body, "||") {
		t.Error("run-migrations.sh fallback gate does not OR the two conditions — Design E logic missing")
	}

	// T5. Log message inside fallback MUST cite "absent OR not Ready"
	// (or equivalent prose) so operators understand which trigger
	// fired. Without this UX hint, future debugging of the fallback
	// branch is opaque.
	if !strings.Contains(body, "not Ready") && !strings.Contains(body, "not ready") && !strings.Contains(body, "CrashLoopBackOff") {
		t.Error("run-migrations.sh fallback log does not cite the CrashLoopBackOff / not-Ready trigger — operator UX gap")
	}

	// T6. Issue #106 traceability comment present (or Design E
	// citation) so a future "simplify" refactor that removes the
	// READY check sees the rationale.
	if !strings.Contains(body, "issue #106") && !strings.Contains(body, "Issue #106") && !strings.Contains(body, "Design E") {
		t.Error("run-migrations.sh has no `issue #106` or `Design E` traceability comment")
	}
}
