// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package repoinvariants

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// PREMISE 0 of .github/scripts/assert_delivery_gate_armed.py decides which
// regime protects the trunk. It used to accept `ci.yml triggers on
// pull_request` as proof that "CI gates the pull request" and return 0 without
// looking at WHAT ci.yml runs — so a workflow that triggered on the PR and ran
// nothing would have certified the trunk as guarded. A trigger is not a gate.
//
// These tests hold the premise to the question it answers: the verdict names
// the gates CI actually runs, and a PR trigger with no gate behind it fails.

func deliveryGateScript(t *testing.T) string {
	t.Helper()
	return filepath.Join(qtestutil.MustRepoRoot(), ".github/scripts/assert_delivery_gate_armed.py")
}

func runDeliveryGateCheck(t *testing.T, root string) (int, string) {
	t.Helper()
	out, err := exec.Command("python3", deliveryGateScript(t), "--root", root).CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	if e, ok := err.(*exec.ExitError); ok {
		return e.ExitCode(), string(out)
	}
	t.Fatalf("could not run the delivery-gate check: %v\n%s", err, out)
	return -1, ""
}

func writeFixtureFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPremiseZeroNamesTheGatesCIRunsOnThisRepository(t *testing.T) {
	root := qtestutil.MustRepoRoot()
	code, out := runDeliveryGateCheck(t, root)
	if code != 0 {
		t.Fatalf("anchor: the check must pass on this repository as it stands; exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "dangling-script-refs") {
		t.Errorf("premise 0 passed without naming the gates ci.yml runs:\n%s", out)
	}
}

func TestPremiseZeroFailsWhenThePullRequestTriggerRunsNoGate(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, ".github/workflows/ci.yml"), `name: CI
on:
  pull_request:
    branches: [develop]
jobs:
  noop:
    runs-on: ubuntu-latest
    steps:
      - run: echo "nothing is checked here"
`)
	code, out := runDeliveryGateCheck(t, root)
	if code == 0 {
		t.Fatalf("a pull_request trigger with no gate behind it was certified as protection:\n%s", out)
	}
	if !strings.Contains(out, "runs no quality gate") {
		t.Errorf("the failure must say CI runs no gate, not fail for an unrelated reason:\n%s", out)
	}
}
