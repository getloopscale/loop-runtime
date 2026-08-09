// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCleanroomMandateOpenBaoSponsorJUNTOSGateDocumented locks the docs
// fix for issue #101 (Cleanroom canonical chain — OpenBao dev-public
// bootstrap not part of mandate).
//
// Pre-fix state: `.claude/rules/cleanroom-up-live-test-mandate.md` listed
// the 6-step chain (Cleanroom → UP → seeds → DOGFOOD → LIVE → sponsor
// JUNTOS browser) without explicitly calling out where the sponsor
// JUNTOS OpenBao Shamir init fits. Operators reading the mandate
// assumed `task setup` provisioned OpenBao automatically — when in
// reality dev-public requires a SEPARATE `loopzero-secrets` cluster +
// 5-share Shamir init that ONLY the sponsor can perform.
//
// Canonical fix: insert Sub-step 2.5 in the mandate between UP (step 2)
// and seeds (step 3) for dev-public env. Document the sponsor JUNTOS
// HARD STOP, the runbook cross-link, and the standalone-multi escape
// hatch via `task k3d:secrets:bootstrap-in-core`.
//
// This test asserts the documentation gate is present, structural, and
// traceable. It does NOT execute any tooling — purely doc body
// inspection.
func TestCleanroomMandateOpenBaoSponsorJUNTOSGateDocumented(t *testing.T) {
	mandatePath := repoFile(t, ".claude", "rules", "cleanroom-up-live-test-mandate.md")
	runbookPath := repoFile(t, "docs", "operations", "openbao-init-runbook.md")

	mandate := readFileText(t, mandatePath)
	runbook := readFileText(t, runbookPath)

	// T1. Mandate body MUST reference Sub-step 2.5 as the canonical
	// header for the new gate. Future refactor that drops or renames
	// the section breaks this test loud.
	if !strings.Contains(mandate, "Sub-step 2.5") {
		t.Error("mandate has no `Sub-step 2.5` section header — issue #101 fix missing")
	}

	// T2. Mandate body MUST mention "Sponsor JUNTOS" + "Shamir" together
	// — those two words anchor the canonical ceremony language. Without
	// both, the gate could be misread as just a chart install.
	if !strings.Contains(mandate, "Sponsor JUNTOS") {
		t.Error("mandate does not mention `Sponsor JUNTOS` for Sub-step 2.5 — issue #101 gate language missing")
	}
	if !strings.Contains(mandate, "Shamir") {
		t.Error("mandate does not mention `Shamir` (the 5-share secret-sharing scheme) — issue #101 ceremony spec missing")
	}

	// T3. The mandate MUST cross-link the runbook (openbao-init-runbook.md)
	// so operators know where the executable ceremony steps live.
	if !strings.Contains(mandate, "openbao-init-runbook.md") {
		t.Error("mandate does not cross-link `docs/operations/openbao-init-runbook.md` — issue #101 traceability requirement")
	}

	// T4. The mandate MUST document the standalone-multi escape hatch
	// (`task k3d:secrets:bootstrap-in-core`) — without this, operators
	// reading the mandate might assume Shamir init is mandatory in ALL
	// envs and block standalone Cleanroom flows.
	if !strings.Contains(mandate, "bootstrap-in-core") {
		t.Error("mandate does not document the standalone-multi escape hatch `task k3d:secrets:bootstrap-in-core` — Cleanroom for standalone-multi might block on missing gate")
	}

	// T5. Mandate MUST cite the env gate condition explicitly so a
	// reader can grep for `LOOPZERO_HELMFILE_ENV=dev-public` and find the
	// canonical reference.
	if !strings.Contains(mandate, "LOOPZERO_HELMFILE_ENV=dev-public") {
		t.Error("mandate does not reference the env gate condition `LOOPZERO_HELMFILE_ENV=dev-public` — issue #101 trigger condition missing")
	}

	// T6. Mandate MUST reference issue #101 for traceability so a future
	// "simplify" refactor that drops the section sees the rationale.
	if !strings.Contains(mandate, "issue #101") && !strings.Contains(mandate, "Issue #101") {
		t.Error("mandate has no `issue #101` traceability comment — future refactor may remove the section without understanding why")
	}

	// T7. Runbook MUST back-link the mandate so the cross-link is
	// bidirectional. A one-way link rots when one side is renamed.
	if !strings.Contains(runbook, "cleanroom-up-live-test-mandate.md") {
		t.Error("runbook does not back-link `cleanroom-up-live-test-mandate.md` — issue #101 bidirectional link missing")
	}

	// T8. Runbook MUST reference Sub-step 2.5 by name so operators
	// arriving via the mandate's Sub-step 2.5 link find the exact
	// section being referenced.
	if !strings.Contains(runbook, "Sub-step 2.5") {
		t.Error("runbook does not reference `Sub-step 2.5` — operators arriving from the mandate cross-link won't find a matching anchor")
	}
}

// repoFile resolves a path relative to the repo root. Test lives at
// infra/tests/scripts/; repo root is 3 levels up.
func repoFile(t *testing.T, parts ...string) string {
	t.Helper()
	rel := append([]string{"..", "..", ".."}, parts...)
	p, err := filepath.Abs(filepath.Join(rel...))
	if err != nil {
		t.Fatalf("resolve repo file %v: %v", parts, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("repo file not found at %s: %v", p, err)
	}
	return p
}

// readFileText reads a file verbatim. Wrapper around os.ReadFile that
// fatals on error (test-style).
func readFileText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
