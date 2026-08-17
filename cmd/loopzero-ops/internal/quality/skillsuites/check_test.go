// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package skillsuites

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// suite writes a skill with a tests/ dir holding one test file.
func suite(t *testing.T, root, skill, body string) {
	t.Helper()
	dir := filepath.Join(root, ".claude", "skills", skill, "tests")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test_x.py"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func requirePytest(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 absent — the gate is advisory here, and so is its test")
	}
	if err := exec.Command("python3", "-c", "import pytest").Run(); err != nil {
		t.Skip("pytest absent")
	}
}

func TestGreenSuitePasses(t *testing.T) {
	requirePytest(t)
	root := t.TempDir()
	suite(t, root, "ok", "def test_true():\n    assert True\n")

	out, err := Check(Config{RepoRoot: root, PerSuiteTimeout: 60 * time.Second})
	if err != nil {
		t.Fatalf("a green suite must pass: %v (%+v)", err, out.Results)
	}
	if len(out.Results) != 1 || !out.Results[0].Passed {
		t.Fatalf("want one passing result; got %+v", out.Results)
	}
}

func TestRedSuiteFails(t *testing.T) {
	requirePytest(t)
	root := t.TempDir()
	suite(t, root, "broken", "def test_false():\n    assert False\n")

	out, err := Check(Config{RepoRoot: root, PerSuiteTimeout: 60 * time.Second})
	if !errors.Is(err, ErrSuitesFailed) {
		t.Fatalf("a red suite must fail the gate: %v", err)
	}
	if f := out.Failed(); len(f) != 1 || f[0].Skill != "broken" {
		t.Fatalf("the failing skill must be named; got %+v", f)
	}
}

// One red among greens must still fail. A gate that reports the majority is a gate that
// lets the minority through.
func TestOneRedAmongGreensStillFails(t *testing.T) {
	requirePytest(t)
	root := t.TempDir()
	suite(t, root, "a", "def test_ok():\n    assert True\n")
	suite(t, root, "b", "def test_bad():\n    assert False\n")
	suite(t, root, "c", "def test_ok():\n    assert True\n")

	out, err := Check(Config{RepoRoot: root, PerSuiteTimeout: 60 * time.Second})
	if !errors.Is(err, ErrSuitesFailed) {
		t.Fatalf("one red must fail the whole gate: %v", err)
	}
	if len(out.Results) != 3 || len(out.Failed()) != 1 {
		t.Fatalf("want 3 results and 1 failure; got %+v", out.Results)
	}
}

// A skill without tests/ is not a failure — plenty of skills are prose only. It is simply
// not a suite, and counting it would make the total meaningless.
func TestSkillWithoutTestsIsNotASuite(t *testing.T) {
	requirePytest(t)
	root := t.TempDir()
	suite(t, root, "has-tests", "def test_ok():\n    assert True\n")
	if err := os.MkdirAll(filepath.Join(root, ".claude", "skills", "prose-only"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := Check(Config{RepoRoot: root, PerSuiteTimeout: 60 * time.Second})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(out.Results) != 1 {
		t.Fatalf("only the skill with tests/ counts; got %+v", out.Results)
	}
}

// Finding nothing is NOT the same as finding nothing wrong. This is the distinction the
// whole gate exists to defend, so it is asserted on the gate itself.
func TestNoSuitesFoundIsAFailureNotASilentPass(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, ErrNoSuitesFound) {
		t.Fatalf("an empty skills tree must fail loudly: %v", err)
	}
}

func TestMissingSkillsTreeIsAFailure(t *testing.T) {
	if _, err := Check(Config{RepoRoot: t.TempDir()}); !errors.Is(err, ErrNoSuitesFound) {
		t.Fatalf("a missing skills tree must fail loudly: %v", err)
	}
}

// A suite that hangs must not hang the delivery. The bound is per suite, so one stuck
// suite does not hide the state of the others.
func TestHangingSuiteIsBoundedAndReportedAsFailure(t *testing.T) {
	requirePytest(t)
	root := t.TempDir()
	suite(t, root, "slow", "import time\n\n\ndef test_sleep():\n    time.sleep(30)\n")

	out, err := Check(Config{RepoRoot: root, PerSuiteTimeout: 2 * time.Second})
	if !errors.Is(err, ErrSuitesFailed) {
		t.Fatalf("a timed-out suite must fail: %v", err)
	}
	if f := out.Failed(); len(f) != 1 || f[0].Tail == "" {
		t.Fatalf("the timeout must be reported, not silent; got %+v", f)
	}
}

// The real tree is the population the gate exists for (B-109): a synthetic mutant proves
// the detector fires; only this proves it fires on what shipped.
func TestRealTreeIsGreen(t *testing.T) {
	requirePytest(t)
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".claude", "skills")); statErr != nil {
		t.Skip("not running inside a repo with an installed ecosystem")
	}
	out, checkErr := Check(Config{RepoRoot: root})
	if out.Advisory {
		t.Skip("pytest absent")
	}
	if checkErr != nil {
		for _, r := range out.Failed() {
			t.Errorf("%s: %s", r.Skill, r.Tail)
		}
		t.Fatalf("%d of %d skill suite(s) red", len(out.Failed()), len(out.Results))
	}
	if len(out.Results) < 10 {
		t.Fatalf("only %d suite(s) discovered — the tree has ~15, so discovery is under-reaching", len(out.Results))
	}
}
