// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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
	// The exemption is real: an out-of-contract exit inside a *.test.ts is not a
	// violation. What changed on 2026-09-05 is the second half of what this test
	// used to assert — that a tree of ONLY exempt files is a pass. It is not: the
	// gate read nothing, and an audit walked that exact shape into a printed PASS
	// over an unread `process.exit`. A real file is scanned here so the exemption
	// is measured against a check that happened.
	root := t.TempDir()
	write(t, root, "cli/src/main.test.ts", "process.exit(9)\n")
	write(t, root, "cli/src/main.ts", "process.exit(0)\n")

	out := check(t, Config{RepoRoot: root})

	if len(out.Violations) != 0 {
		t.Fatalf("the *.test.ts exit must be ignored: %v", out.Violations)
	}
	if out.ScannedFiles != 1 {
		t.Fatalf("exactly the non-test file should be read; got %d", out.ScannedFiles)
	}
	if !out.Pass() {
		t.Fatalf("one in-contract file was read and nothing was wrong: %+v", out)
	}
}

func TestCheck_SubjectAbsentAndDeclared_IsNotAPass(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/main.go", "package main\n")
	writePin(t, root, "cli/src", "measured: extracted to loop-runtime-cli in 686b9a4c5, no checkout beside")

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
		t.Fatalf("cloning loop-runtime-cli beside the repo must retire the pin, loudly: %+v", out)
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

// --- B-190: the SUBJECT field of a pinned line is measured, every run -------

// writePinLines writes the pin with an arbitrary number of already-formatted
// `subject\twhy` lines, after the same two header lines writePin uses, so a
// line number in a violation is checkable against a known offset.
func writePinLines(t *testing.T, root string, lines ...string) {
	t.Helper()
	write(t, root, DefaultPinFile, "# header comment\n\n"+strings.Join(lines, "\n")+"\n")
}

// The oracle from B-190's evidence, run against the code instead of the binary:
// a pin line naming a subject that is RIGHT HERE. Before this, the subject field
// was never stat'd — only the two hardcoded candidates were — so the gate printed
// "docs is not in this tree and not in any checkout beside it" and exited 0.
func TestCheck_PinnedSubjectThatExists_Fails(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/index.md", "# docs\n")
	writePinLines(t, root,
		"cli/src\tmeasured: extracted to loop-runtime-cli in 686b9a4c5, no checkout beside",
		"docs\tthis subject is right here and present")

	out := check(t, Config{RepoRoot: root, CLIRoot: filepath.Join(root, "no-such-sibling")})

	if out.Pass() {
		t.Fatalf("a pin the filesystem contradicts must never pass: %+v", out)
	}
	if len(out.Violations) != 1 {
		t.Fatalf("exactly the `docs` line must fail: %v", out.Violations)
	}
	v := out.Violations[0]
	for _, want := range []string{DefaultPinFile, ":4:", "docs", filepath.Join(root, "docs"), "delete that line"} {
		if !strings.Contains(v, want) {
			t.Fatalf("the failure must name the pin, the line, the subject and the path it found (%q): %s", want, v)
		}
	}
	// And the false sentence is not printed at all: `docs` never becomes an
	// absence the runner would announce.
	if len(out.TrackedAbsences) != 1 || out.TrackedAbsences[0].Subject != "cli/src" {
		t.Fatalf("only the genuinely absent subject may be carried as an absence: %+v", out.TrackedAbsences)
	}
}

// The declaration travels with the path that produced it, so the printed line
// is a measurement rather than prose about the filesystem.
func TestCheck_TrackedAbsenceCarriesThePathItStatd(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/main.go", "package main\n")
	writePin(t, root, "cli/src", "measured: extracted to loop-runtime-cli, no checkout beside")

	out := check(t, Config{RepoRoot: root})

	if len(out.TrackedAbsences) != 1 {
		t.Fatalf("the declared absence must survive: %+v", out.TrackedAbsences)
	}
	got := out.TrackedAbsences[0].ResolvedPath
	if want := filepath.Join(root, "cli", "src"); got != want {
		t.Fatalf("the absence must name where it was looked for: got %q want %q", got, want)
	}
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Fatalf("the path named as absent must really be absent (err=%v)", err)
	}
}

// A pin whose only line names a present subject declares NO absence: the CLI
// tree is still missing and still undeclared, which is the B-185 failure, and
// the stale line is the B-190 one. Both are true and both are printed.
func TestCheck_PinWithOnlyAPresentSubject_LeavesTheAbsenceUndeclared(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/index.md", "# docs\n")
	writePinLines(t, root, "docs\tthis subject is right here and present")

	out := check(t, Config{RepoRoot: root, CLIRoot: filepath.Join(root, "no-such-sibling")})

	if len(out.Violations) != 2 {
		t.Fatalf("the stale line AND the undeclared absence must both be reported: %v", out.Violations)
	}
	joined := strings.Join(out.Violations, "\n")
	for _, want := range []string{"but it EXISTS at", "declares no absence"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in: %s", want, joined)
		}
	}
	if len(out.TrackedAbsences) != 0 {
		t.Fatalf("a present subject is not an absence: %+v", out.TrackedAbsences)
	}
}

// A subject that comes back AND a source tree that turns up are two different
// facts; the stale line must not be reported twice for one line.
func TestCheck_SubjectPresentAndTreeFound_ReportsOneViolationPerLine(t *testing.T) {
	root := t.TempDir()
	write(t, root, "cli/src/a.ts", "process.exit(0)\n")
	writePin(t, root, "cli/src", "stale prose: says gone, tree says otherwise")

	out := check(t, Config{RepoRoot: root})

	if len(out.Violations) != 1 {
		t.Fatalf("one stale line is one violation, not two: %v", out.Violations)
	}
	if out.ScannedFiles != 0 {
		t.Fatalf("a contradicted pin is fixed before this gate scans; got %d file(s)", out.ScannedFiles)
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
