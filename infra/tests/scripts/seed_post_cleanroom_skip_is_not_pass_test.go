// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// runStepOutcome executes the script's real run_step against a stub step and reports
// the RESULTS entry and counters it produced.
func runStepOutcome(t *testing.T, stepOutput string, stepExit int) string {
	t.Helper()
	body := readScript(t, "seed-post-cleanroom-all.sh")
	m := regexp.MustCompile(`(?s)run_step\(\) \{.*?\n\}`).FindString(body)
	if m == "" {
		t.Fatal("run_step function not found in script")
	}
	harness := `set -uo pipefail
DRY_RUN=0; PASS=0; FAIL=0; SKIP=0; RESULTS=()
log() { :; }
` + m + `
stub() { printf '%s\n' "$STUB_OUT"; return $STUB_RC; }
run_step "9-stub" stub
printf '%s|PASS=%d FAIL=%d SKIP=%d\n' "${RESULTS[0]}" "$PASS" "$FAIL" "$SKIP"`
	cmd := exec.Command("bash", "-c", harness)
	cmd.Env = append(cmd.Environ(), "STUB_OUT="+stepOutput, "STUB_RC="+map[bool]string{true: "0", false: "1"}[stepExit == 0])
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// A seed that skips exits 0 because skipping is not an error — but the chain counted
// that exit as PASS. On the 2026-09-29 cleanroom, seed admin-loop-runtime-dev printed
// "SKIP — KRATOS_ADMIN_URL not set", the summary said PASS, and the two steps that need
// the admin session failed with no hint that the cause was upstream (#453).
func TestASkippedSeedIsCountedAsSkippedNotPassed(t *testing.T) {
	got := runStepOutcome(t, "seed admin-loop-runtime-dev: SKIP — KRATOS_ADMIN_URL not set", 0)
	if !strings.HasPrefix(got, "SKIP: 9-stub") || !strings.Contains(got, "PASS=0 FAIL=0 SKIP=1") {
		t.Errorf("a skipping seed was not recorded as SKIP: %q", got)
	}
	if !strings.Contains(got, "KRATOS_ADMIN_URL not set") {
		t.Errorf("the SKIP entry lost the seed's reason: %q", got)
	}
}

func TestAPassingSeedIsStillCountedAsPassed(t *testing.T) {
	got := runStepOutcome(t, "seed minio: PASS — bucket ready", 0)
	if !strings.HasPrefix(got, "PASS: 9-stub") || !strings.Contains(got, "PASS=1 FAIL=0 SKIP=0") {
		t.Errorf("a passing seed was not recorded as PASS: %q", got)
	}
}

func TestAFailingSeedIsStillCountedAsFailed(t *testing.T) {
	got := runStepOutcome(t, "seed broker: FAIL — boom", 1)
	if !strings.HasPrefix(got, "FAIL: 9-stub") || !strings.Contains(got, "PASS=0 FAIL=1 SKIP=0") {
		t.Errorf("a failing seed was not recorded as FAIL: %q", got)
	}
}
