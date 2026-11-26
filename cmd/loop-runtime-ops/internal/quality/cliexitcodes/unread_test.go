package cliexitcodes

import (
	"os"
	"path/filepath"
	"testing"
)

// FOUND IS NOT READ, and the gate's headline invariant conflated them.
//
// `Pass()` was `Checked && no violations`, and `Checked` became true the moment a
// directory was located — before a single file was opened. So a subject that exists
// and yields nothing produced:
//
//	cli-exit-codes: PASS — 0 file(s) under <path>, all exit codes within the contract
//
// An audit walked the gate's OWN PRESCRIBED WORKFLOW into that state on 2026-09-05.
// loop-runtime-cli appears vendored under node_modules with `process.exit(42)` in it; the
// gate FAILS and tells the operator the absence has ended and to delete the pin
// line; the operator does exactly that; and the next run prints PASS over zero files
// with the offending call unread.
//
// That is strictly worse than the state B-185 was opened to fix. The old SKIP at
// least said something.

func TestAFoundButUnreadTreeIsNotAPass(t *testing.T) {
	root := t.TempDir()
	cli := filepath.Join(root, "cli")
	if err := os.MkdirAll(filepath.Join(cli, "src"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	o := check(t, Config{RepoRoot: root, CLIRoot: cli})

	if o.Pass() {
		t.Fatalf("the directory exists and nothing in it was read; Pass()=true says a "+
			"check happened. Checked=%v ScannedFiles=%d", o.Checked, o.ScannedFiles)
	}
}

func TestATreeOfOnlyExemptFilesIsNotAPass(t *testing.T) {
	// The likelier live shape: the directory is real and everything in it is
	// excluded, so the scanner reads zero files and reports on none of them.
	root := t.TempDir()
	cli := filepath.Join(root, "cli")
	write(t, cli, "src/main.test.ts", "process.exit(9)\n")
	write(t, cli, "src/util.spec.ts", "process.exit(9)\n")

	o := check(t, Config{RepoRoot: root, CLIRoot: cli})

	if o.Pass() {
		t.Fatalf("every file was exempt, so nothing was read; Pass()=true. "+
			"ScannedFiles=%d", o.ScannedFiles)
	}
}

func TestOneReadFileIsEnoughToBeACheck(t *testing.T) {
	// The other direction, so the fix is a floor and not a blanket refusal.
	root := t.TempDir()
	cli := filepath.Join(root, "cli")
	write(t, cli, "src/main.ts", "process.exit(0)\n")

	o := check(t, Config{RepoRoot: root, CLIRoot: cli})

	if !o.Pass() {
		t.Fatalf("one in-contract file was read and the gate did not pass: %+v", o)
	}
	if o.ScannedFiles != 1 {
		t.Fatalf("want 1 scanned file, got %d", o.ScannedFiles)
	}
}

func TestFoundButUnreadIsDistinguishableFromNoSubjectAtAll(t *testing.T) {
	// The runner prints PARTIAL for both, and it should — a reader needs the same
	// fact either way. But the two states must stay tellable apart, or the message
	// cannot say which one happened.
	root := t.TempDir()
	cli := filepath.Join(root, "cli")
	if err := os.MkdirAll(filepath.Join(cli, "src"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	found := check(t, Config{RepoRoot: root, CLIRoot: cli})
	// A SEPARATE root: pointing CLIRoot at nothing while RepoRoot still holds
	// cli/src let the default candidate answer, so the "absent" case was not
	// absent. The first draft of this test asserted against its own fixture.
	empty := t.TempDir()
	absent := check(t, Config{RepoRoot: empty, CLIRoot: filepath.Join(empty, "nowhere")})

	if !found.Checked {
		t.Fatal("the directory exists, so Checked must be true — that is the fact the message uses")
	}
	if absent.Checked {
		t.Fatal("no directory, so Checked must be false")
	}
	if found.Pass() || absent.Pass() {
		t.Fatal("neither read anything, so neither is a pass")
	}
}
