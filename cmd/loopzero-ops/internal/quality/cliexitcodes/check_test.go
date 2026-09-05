// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package cliexitcodes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// writePin writes the declared-absence pin with one subject line.
func writePin(t *testing.T, root, subject, why string) {
	t.Helper()
	write(t, root, DefaultPinFile, "# header comment\n\n"+subject+"\t"+why+"\n")
}

func check(t *testing.T, cfg Config) *Outcome {
	t.Helper()
	out, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	return out
}

// --- the contract itself, on a source tree that exists ---------------------

func TestCheck_ContractCodes_Pass(t *testing.T) {
	root := t.TempDir()
	write(t, root, "cli/src/a.ts", "process.exit(0)\nprocess.exit(3)\nprocess.exit(5)\n")
	out := check(t, Config{RepoRoot: root})
	if !out.Pass() {
		t.Fatalf("exit codes 0-5 must PASS: %+v", out)
	}
	if !out.Checked || out.ScannedFiles != 1 {
		t.Fatalf("a scan that happened must say so: Checked=%v ScannedFiles=%d", out.Checked, out.ScannedFiles)
	}
}

func TestCheck_OutOfContract_Fails(t *testing.T) {
	root := t.TempDir()
	write(t, root, "cli/src/a.ts", "process.exit(6)\n")
	out := check(t, Config{RepoRoot: root})
	if out.Pass() || len(out.Violations) != 1 {
		t.Fatalf("process.exit(6) must FAIL: %+v", out)
	}
	if !strings.Contains(out.Violations[0], "6") || !strings.Contains(out.Violations[0], "cli/src/a.ts") {
		t.Fatalf("violation must name the code and the file: %v", out.Violations)
	}
}

func TestCheck_SignalHandler_Pass(t *testing.T) {
	root := t.TempDir()
	write(t, root, "cli/src/a.ts", "process.on('SIGINT', () => {\n  cleanup()\n  process.exit(130)\n})\n")
	out := check(t, Config{RepoRoot: root})
	if !out.Pass() {
		t.Fatalf("exit(130) inside a SIG handler must PASS: %+v", out)
	}
}

func TestCheck_SignalCodeOutsideHandler_Fails(t *testing.T) {
	root := t.TempDir()
	write(t, root, "cli/src/a.ts", "function f() {\n  doStuff()\n  somethingElse()\n  process.exit(143)\n}\n")
	out := check(t, Config{RepoRoot: root})
	if out.Pass() {
		t.Fatalf("exit(143) NOT in a SIG handler must FAIL: %+v", out)
	}
}

func TestCheck_TestFilesIgnored(t *testing.T) {
	root := t.TempDir()
	write(t, root, "cli/src/a.test.ts", "process.exit(99)\n")
	write(t, root, "cli/src/__tests__/b.ts", "process.exit(98)\n")
	out := check(t, Config{RepoRoot: root})
	if !out.Pass() {
		t.Fatalf("test files must be ignored: %+v", out)
	}
	if out.ScannedFiles != 0 {
		t.Fatalf("exempt files must not be counted as scanned: %d", out.ScannedFiles)
	}
}

// --- B-185: what the gate may claim when the subject is not here -----------

// The defect: an absent subject used to be Pass:true, Skipped:true, exit 0.
func TestCheck_SubjectAbsentAndDeclared_IsNotAPass(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/main.go", "package main\n")
	writePin(t, root, "cli/src", "measured: extracted to loopzero-cli in 686b9a4c5, no checkout beside")

	out := check(t, Config{RepoRoot: root})

	if out.Pass() {
		t.Fatalf("an absent subject must NEVER report Pass: %+v", out)
	}
	if out.Checked {
		t.Fatalf("nothing was scanned, so Checked must be false: %+v", out)
	}
	if len(out.Violations) != 0 {
		t.Fatalf("a DECLARED absence is not a violation: %v", out.Violations)
	}
	if len(out.TrackedAbsences) != 1 || out.TrackedAbsences[0].Subject != "cli/src" {
		t.Fatalf("the declared absence must be carried out for printing: %+v", out.TrackedAbsences)
	}
	if !strings.Contains(out.TrackedAbsences[0].Why, "686b9a4c5") {
		t.Fatalf("the measurement behind the absence must travel with it: %q", out.TrackedAbsences[0].Why)
	}
}

// An absence nobody wrote down is the bug, not the baseline.
func TestCheck_SubjectAbsentAndUndeclared_Fails(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/main.go", "package main\n")

	out := check(t, Config{RepoRoot: root})

	if out.Pass() || len(out.Violations) != 1 {
		t.Fatalf("an undeclared absence must FAIL: %+v", out)
	}
	v := out.Violations[0]
	for _, want := range []string{"no CLI source tree", DefaultPinFile} {
		if !strings.Contains(v, want) {
			t.Fatalf("the failure must name what is missing and where to declare it (%q): %s", want, v)
		}
	}
}

// The B-183 property: when the subject comes BACK, the declaration must fail
// rather than quietly keep declaring.
func TestCheck_SubjectReturnsInRepo_StalePinFails(t *testing.T) {
	root := t.TempDir()
	write(t, root, "cli/src/a.ts", "process.exit(0)\n")
	writePin(t, root, "cli/src", "stale prose: says gone, tree says otherwise")

	out := check(t, Config{RepoRoot: root})

	if out.Pass() || len(out.Violations) != 1 {
		t.Fatalf("a pin that outlives its absence must FAIL: %+v", out)
	}
	v := out.Violations[0]
	for _, want := range []string{DefaultPinFile, ":3:", "cli/src", "delete that line"} {
		if !strings.Contains(v, want) {
			t.Fatalf("the failure must name the pin, the line and the fix (%q): %s", want, v)
		}
	}
}

// Same property through the sibling checkout — the trigger that is actually
// plausible, since cli/ was extracted on purpose and is not coming home.
func TestCheck_SubjectReturnsInSibling_StalePinFails(t *testing.T) {
	root := t.TempDir()
	cli := t.TempDir()
	write(t, root, "api/main.go", "package main\n")
	write(t, cli, "src/a.ts", "process.exit(0)\n")
	writePin(t, root, "cli/src", "stale prose")

	out := check(t, Config{RepoRoot: root, CLIRoot: cli})

	if out.Pass() || len(out.Violations) != 1 {
		t.Fatalf("cloning loopzero-cli beside the repo must retire the pin, loudly: %+v", out)
	}
	if !strings.Contains(out.Violations[0], filepath.Join(cli, "src")) {
		t.Fatalf("the failure must name the tree that appeared: %s", out.Violations[0])
	}
}

// With the pin retired, the sibling checkout is scanned for real: the gate can
// go back to checking without any code change.
func TestCheck_SiblingScannedWhenPinRetired(t *testing.T) {
	root := t.TempDir()
	cli := t.TempDir()
	write(t, root, "api/main.go", "package main\n")
	write(t, cli, "src/a.ts", "process.exit(7)\n")

	out := check(t, Config{RepoRoot: root, CLIRoot: cli})

	if !out.Checked || out.ScannedFiles != 1 {
		t.Fatalf("the sibling checkout must actually be walked: %+v", out)
	}
	if out.Pass() || len(out.Violations) != 1 {
		t.Fatalf("process.exit(7) in the sibling must FAIL: %+v", out)
	}
	if !strings.Contains(out.Violations[0], "process.exit(7)") {
		t.Fatalf("violation must name the code: %s", out.Violations[0])
	}
}

// The in-repo subject wins the search, so a CLI that comes home is what gets
// checked even with a sibling on disk.
func TestCheck_InRepoSubjectWinsOverSibling(t *testing.T) {
	root := t.TempDir()
	cli := t.TempDir()
	write(t, root, "cli/src/a.ts", "process.exit(0)\n")
	write(t, cli, "src/b.ts", "process.exit(9)\n")

	out := check(t, Config{RepoRoot: root, CLIRoot: cli})

	if !out.Pass() {
		t.Fatalf("the in-repo tree is clean, so the gate must pass: %+v", out)
	}
	if out.ScannedRoot != filepath.Join(root, "cli", "src") {
		t.Fatalf("in-repo cli/src must be searched first: %q", out.ScannedRoot)
	}
}

// The output must be able to say WHERE it looked, not just that it found
// nothing — the measurement that a SKIP never carried.
func TestCheck_SearchedPathsAreReported(t *testing.T) {
	root := t.TempDir()
	writePin(t, root, "cli/src", "declared")

	out := check(t, Config{RepoRoot: root})

	if len(out.SearchedPaths) != 2 {
		t.Fatalf("both candidates must be reported: %v", out.SearchedPaths)
	}
	if out.SearchedPaths[0] != filepath.Join(root, "cli", "src") {
		t.Fatalf("first candidate must be the in-repo subject: %v", out.SearchedPaths)
	}
	if out.SearchedPaths[1] != filepath.Join(root, DefaultCLIDir, "src") {
		t.Fatalf("second candidate must be the default sibling: %v", out.SearchedPaths)
	}
}

func TestParsePin_MalformedLine_IsAnError(t *testing.T) {
	root := t.TempDir()
	write(t, root, DefaultPinFile, "cli/src has no tab at all\n")

	if _, err := Check(Config{RepoRoot: root}); err == nil {
		t.Fatal("a pin line the gate cannot parse must be an error, not a silent empty pin")
	}
}

func TestParsePin_CommentsAndBlanksIgnored(t *testing.T) {
	root := t.TempDir()
	write(t, root, DefaultPinFile, "# why\n#\n\ncli/src\tdeclared\n")

	out := check(t, Config{RepoRoot: root})
	if len(out.TrackedAbsences) != 1 || out.TrackedAbsences[0].Line != 4 {
		t.Fatalf("comments and blanks must not become absences, and the line number must be real: %+v", out.TrackedAbsences)
	}
}
