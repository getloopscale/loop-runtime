// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package deploytimelinelive replaces the bash hermetic structural
// check for infra/tests/e2e/dev-public-deploy-timeline_test.sh with a
// homed Go test — per plan 2026-05-23-operational-logic-capability-
// architecture, hermetic structural checks belong in pkg/quality not in
// infra/tests/*.sh. The original bash check
// (check-dev-public-deploy-timeline-syntax_test.sh) was removed in the
// same commit that landed this Go gate.
package deploytimelinelive

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// liveScriptPath resolves the LIVE bash script via VCS root.
func liveScriptPath(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("git rev-parse failed (not in a git tree?): %v", err)
	}
	root := strings.TrimSpace(string(out))
	return filepath.Join(root, "infra", "tests", "e2e", "dev-public-deploy-timeline_test.sh")
}

func readScript(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(liveScriptPath(t))
	if err != nil {
		t.Fatalf("read LIVE script: %v", err)
	}
	return string(body)
}

// TestLiveScript_BashSyntaxClean runs `bash -n` to confirm the LIVE
// orchestrator parses cleanly.
func TestLiveScript_BashSyntaxClean(t *testing.T) {
	cmd := exec.Command("bash", "-n", liveScriptPath(t))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("bash -n FAIL: %v\n%s", err, stderr.String())
	}
}

// TestLiveScript_HasStrictMode confirms set -euo pipefail.
func TestLiveScript_HasStrictMode(t *testing.T) {
	if !strings.Contains(readScript(t), "set -euo pipefail") {
		t.Errorf("LIVE script missing `set -euo pipefail` strict mode")
	}
}

// TestLiveScript_ScenarioFunctionsPresent locks the 5 canonical scenarios.
func TestLiveScript_ScenarioFunctionsPresent(t *testing.T) {
	body := readScript(t)
	required := []string{
		"scenario_A()",
		"scenario_B()",
		"scenario_C()",
		"scenario_D()",
		"preflight_E()",
	}
	for _, fn := range required {
		if !strings.Contains(body, fn) {
			t.Errorf("missing scenario function %q", fn)
		}
	}
}

// TestLiveScript_FlagsWired confirms the 4 canonical flags exist.
func TestLiveScript_FlagsWired(t *testing.T) {
	body := readScript(t)
	flags := []string{"--skip-live", "--scenario=", "--json", "--help"}
	for _, f := range flags {
		if !strings.Contains(body, f) {
			t.Errorf("missing canonical flag %q", f)
		}
	}
}

// TestLiveScript_OperatorMandateCited per cleanroom-up-live-test-mandate.md
// HARD STOP must be cited inline.
func TestLiveScript_OperatorMandateCited(t *testing.T) {
	body := readScript(t)
	if !strings.Contains(body, "OPERATOR HARD STOP") {
		t.Errorf("LIVE script missing OPERATOR HARD STOP citation")
	}
	if !strings.Contains(body, "cleanroom-up-live-test-mandate") {
		t.Errorf("LIVE script missing cleanroom-up-live-test-mandate cross-reference")
	}
	if !strings.Contains(body, "deploy-timeline-canonical-5-phases") {
		t.Errorf("LIVE script missing plan slug citation")
	}
	if !strings.Contains(body, "local-dogfood-token.txt") {
		t.Errorf("LIVE script missing JWT seed file documentation")
	}
}

// TestLiveScript_DenylistCoverage confirms scenario D probes denylist
// terms (proxy: term appears in script source).
func TestLiveScript_DenylistCoverage(t *testing.T) {
	body := readScript(t)
	denylist := []string{"ArgoCD", "cosign", "Gitea"}
	for _, term := range denylist {
		if !strings.Contains(body, term) {
			t.Errorf("LIVE script scenario D missing denylist probe: %q", term)
		}
	}
}

// TestLiveScript_TenantDefenseProbe confirms scenario B has 404 +
// foreign-deploy probe shape.
func TestLiveScript_TenantDefenseProbe(t *testing.T) {
	body := readScript(t)
	if !strings.Contains(body, "404") {
		t.Errorf("LIVE script missing 404 tenant-defense assertion")
	}
	if !strings.Contains(body, "dep-does-not-exist") {
		t.Errorf("LIVE script missing foreign-deploy probe pattern")
	}
}

// TestLiveScript_NoAntiPatterns enforces canonical commit hygiene:
// no Co-Authored-By trailer + no --no-verify references.
func TestLiveScript_NoAntiPatterns(t *testing.T) {
	body := readScript(t)
	if strings.Contains(body, "Co-Authored-By") {
		t.Errorf("LIVE script contains Co-Authored-By trailer reference")
	}
	if strings.Contains(body, "--no-verify") {
		t.Errorf("LIVE script contains --no-verify (hook-bypass anti-pattern)")
	}
}

// TestLiveScript_ExitCodeReflectsFailCount confirms the verdict
// boilerplate at end exits non-zero on FAIL.
func TestLiveScript_ExitCodeReflectsFailCount(t *testing.T) {
	body := readScript(t)
	if !strings.Contains(body, "[[ $FAIL -eq 0 ]]") {
		t.Errorf("LIVE script missing canonical [[ $FAIL -eq 0 ]] exit-code guard")
	}
}
