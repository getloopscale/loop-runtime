// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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

// pytestAvailable reports whether the interpreter this gate shells out to is present.
//
// Nothing here SKIPS on its absence, deliberately. `Check` documents that it returns
// `Advisory: true` when pytest is missing, and that is a contract worth asserting — a
// test that vanishes in that environment leaves the advisory path unproven exactly where
// it is the only path that runs. So each test branches and asserts on both sides.
func pytestAvailable() bool {
	if _, err := exec.LookPath("python3"); err != nil {
		return false
	}
	return exec.Command("python3", "-c", "import pytest").Run() == nil
}

// assertAdvisory locks the documented no-interpreter contract: advisory, and NOT an error
// — blocking a delivery on a tool the operator may legitimately not have installed is how
// a gate gets bypassed.
func assertAdvisory(t *testing.T, out Outcome, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("without pytest the gate must be advisory, not an error: %v", err)
	}
	if !out.Advisory {
		t.Fatal("without pytest the gate must mark itself advisory, so a reader knows nothing was concluded")
	}
	if len(out.Results) != 0 {
		t.Fatalf("without pytest no suite can have run; got %+v", out.Results)
	}
}

func TestGreenSuitePasses(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "ok", "def test_true():\n    assert True\n")

	out, err := violationOf(Check(Config{RepoRoot: root, PerSuiteTimeout: 60 * time.Second}))
	if !pytestAvailable() {
		assertAdvisory(t, out, err)
		return
	}
	if err != nil {
		t.Fatalf("a green suite must pass: %v (%+v)", err, out.Results)
	}
	if len(out.Results) != 1 || !out.Results[0].Passed {
		t.Fatalf("want one passing result; got %+v", out.Results)
	}
}

func TestRedSuiteFails(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "broken", "def test_false():\n    assert False\n")

	out, err := violationOf(Check(Config{RepoRoot: root, PerSuiteTimeout: 60 * time.Second}))
	if !pytestAvailable() {
		assertAdvisory(t, out, err)
		return
	}
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
	root := t.TempDir()
	suite(t, root, "a", "def test_ok():\n    assert True\n")
	suite(t, root, "b", "def test_bad():\n    assert False\n")
	suite(t, root, "c", "def test_ok():\n    assert True\n")

	out, err := violationOf(Check(Config{RepoRoot: root, PerSuiteTimeout: 60 * time.Second}))
	if !pytestAvailable() {
		assertAdvisory(t, out, err)
		return
	}
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
	root := t.TempDir()
	suite(t, root, "has-tests", "def test_ok():\n    assert True\n")
	if err := os.MkdirAll(filepath.Join(root, ".claude", "skills", "prose-only"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := violationOf(Check(Config{RepoRoot: root, PerSuiteTimeout: 60 * time.Second}))
	if !pytestAvailable() {
		assertAdvisory(t, out, err)
		return
	}
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
	if _, err := violationOf(Check(Config{RepoRoot: root})); !errors.Is(err, ErrNoSuitesFound) {
		t.Fatalf("an empty skills tree must fail loudly: %v", err)
	}
}

// A missing skills tree is the installed kit being absent, which .gitignore makes
// the state of every fresh clone. It is reported by a name distinct from a tree
// that is present and holds no suite (which fails); its verdict is Skip.
func TestMissingSkillsTreeIsReportedByName(t *testing.T) {
	if _, err := Discover(Config{RepoRoot: t.TempDir()}); !errors.Is(err, ErrSkillsRootMissing) {
		t.Fatalf("a missing skills tree must be reported as such: %v", err)
	}
}

// A suite that hangs must not hang the delivery. The bound is per suite, so one stuck
// suite does not hide the state of the others.
func TestHangingSuiteIsBoundedAndReportedAsFailure(t *testing.T) {
	root := t.TempDir()
	suite(t, root, "slow", "import time\n\n\ndef test_sleep():\n    time.sleep(30)\n")

	out, err := violationOf(Check(Config{RepoRoot: root, PerSuiteTimeout: 2 * time.Second}))
	if !pytestAvailable() {
		assertAdvisory(t, out, err)
		return
	}
	if !errors.Is(err, ErrSuitesFailed) {
		t.Fatalf("a timed-out suite must fail: %v", err)
	}
	if f := out.Failed(); len(f) != 1 || f[0].Tail == "" {
		t.Fatalf("the timeout must be reported, not silent; got %+v", f)
	}
}

// The real tree is the population the gate exists for (B-109), and DISCOVERY is the half a
// unit test can uniquely assert about it. Running the fifteen suites is what the gate does
// in `task quality:gates`; doing it here too would put ~40s and the health of fifteen
// unrelated suites into every `go test ./...` of this module — which is how a package's
// result starts depending on who else is red.
func TestRealTreeDiscoveryReachesTheSuites(t *testing.T) {
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatal(err)
	}

	found, discErr := Discover(Config{RepoRoot: root})

	// No ecosystem installed: the contract is a LOUD refusal, and asserting it beats
	// vanishing — it proves the gate cannot report green over a tree it never found.
	if _, statErr := os.Stat(filepath.Join(root, ".claude", "skills")); statErr != nil {
		if !errors.Is(discErr, ErrSkillsRootMissing) {
			t.Fatalf("without a skills tree discovery must fail loudly: %v", discErr)
		}
		return
	}
	if discErr != nil {
		t.Fatalf("discovery failed on the real tree: %v", discErr)
	}
	if len(found) < 10 {
		t.Fatalf("only %d suite(s) discovered — the tree carries ~15, so discovery is under-reaching", len(found))
	}
}
