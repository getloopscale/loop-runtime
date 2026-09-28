// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package nomigrateforce

import (
	"os"
	"path/filepath"
	"testing"
)

// w writes a fixture file (creating parent dirs) into the temp root.
func w(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// blocks runs Check and asserts the gate would block (Pass == false).
func blocks(t *testing.T, root string) bool {
	t.Helper()
	o, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return !o.Pass
}

// The fixture content below intentionally contains the forbidden phrase as
// test data; the gate's own test file is skipped via DefaultSkipSuffixes when
// scanning the live repo, so this does not self-flag.
const forbidden = "migrate" + " force"

func TestBareMigrateForceInProdBlocks(t *testing.T) {
	root := t.TempDir()
	w(t, root, "run.sh", `echo "`+forbidden+` 5"`+"\n")
	if !blocks(t, root) {
		t.Error("expected block")
	}
}

func TestCommentedMigrateForceBlocks(t *testing.T) {
	root := t.TempDir()
	w(t, root, "run.sh", "# Documentation: do not "+forbidden+" in production\n")
	if !blocks(t, root) {
		t.Error("expected block for bare comment without marker")
	}
}

func TestAllowlistMarker1LineAbovePasses(t *testing.T) {
	root := t.TempDir()
	w(t, root, "heal.sh", "# allowlist: "+forbidden+" (heal recovery)\necho \"Run: "+forbidden+" 5\"\n")
	if blocks(t, root) {
		t.Error("expected pass — marker 1 line above")
	}
}

func TestAllowlistMarker5LinesAboveBlocks(t *testing.T) {
	root := t.TempDir()
	w(t, root, "heal.sh", "# allowlist: "+forbidden+" (out of range)\nf1\nf2\nf3\nf4\necho \"Run: "+forbidden+" 5\"\n")
	if !blocks(t, root) {
		t.Error("expected block — marker 5 lines above is out of range")
	}
}

func TestEmptyInputPasses(t *testing.T) {
	root := t.TempDir()
	w(t, root, "clean.sh", "echo hello\n")
	if blocks(t, root) {
		t.Error("expected pass — no matches")
	}
}

func TestMixedAllowlistedAndUnmarkedBlocks(t *testing.T) {
	root := t.TempDir()
	w(t, root, "heal.sh", "# allowlist: "+forbidden+" (OK)\necho \""+forbidden+" 5\"\n")
	w(t, root, "bad.sh", "# docs without marker\necho \""+forbidden+" 6\"\n")
	if !blocks(t, root) {
		t.Error("expected block — bad.sh has no marker")
	}
}

func TestCaseInsensitiveMatching(t *testing.T) {
	root := t.TempDir()
	w(t, root, "loud.sh", `echo "MIGRATE FORCE 5"`+"\n")
	if !blocks(t, root) {
		t.Error("expected block — case-insensitive match")
	}
}

func TestPatternInsideQuotedStringBlocks(t *testing.T) {
	root := t.TempDir()
	w(t, root, "cmd.sh", `CMD="`+forbidden+` 5"`+"\n")
	if !blocks(t, root) {
		t.Error("expected block — pattern in quoted string")
	}
}

func TestAllowlistMarkerSameLinePasses(t *testing.T) {
	root := t.TempDir()
	w(t, root, "heal.sh", `echo "Run `+forbidden+`"  # allowlist: `+forbidden+` (inline)`+"\n")
	if blocks(t, root) {
		t.Error("expected pass — same-line marker")
	}
}

func TestExcludedPathsSkipped(t *testing.T) {
	root := t.TempDir()
	w(t, root, "node_modules/x.sh", `echo "`+forbidden+` 5"`+"\n")
	w(t, root, ".git/y.sh", `echo "`+forbidden+` 5"`+"\n")
	w(t, root, "docs/z.md", `echo "`+forbidden+` 5"`+"\n")
	if blocks(t, root) {
		t.Error("expected pass — all matches in excluded dirs")
	}
}

func TestScansGoPythonJSONFilesAlso(t *testing.T) {
	root := t.TempDir()
	w(t, root, "main.go", `const cmd = "`+forbidden+` 5"`+"\n")
	w(t, root, "script.py", `cmd = "`+forbidden+` 5"`+"\n")
	w(t, root, "config.json", `{"cmd": "`+forbidden+` 5"}`+"\n")
	if !blocks(t, root) {
		t.Error("expected block — scans .go/.py/.json")
	}
}

func TestGoStyleAllowlistCommentPasses(t *testing.T) {
	root := t.TempDir()
	w(t, root, "heal.go", "// allowlist: "+forbidden+" (heal recovery)\nconst cmd = \""+forbidden+" 5\"\n")
	if blocks(t, root) {
		t.Error("expected pass — Go-style // allowlist marker")
	}
}

func TestYAMLBlockScalarWithMigrateForceBlocks(t *testing.T) {
	root := t.TempDir()
	w(t, root, "workflow.yml", "description: |\n  runs:\n    "+forbidden+" 5\n  done.\n")
	if !blocks(t, root) {
		t.Error("expected block — YAML block scalar")
	}
}

func TestMigrateWithFlagsBetweenBlocks(t *testing.T) {
	root := t.TempDir()
	// Flags (no values) between migrate and force are matched by (\s+-\S+)*.
	w(t, root, "run.sh", `migrate -verbose force 5`+"\n")
	if !blocks(t, root) {
		t.Error("expected block — flag between migrate and force")
	}
}

func TestBinaryFileSkipped(t *testing.T) {
	root := t.TempDir()
	// NUL byte makes it binary; content references the phrase but is skipped.
	w(t, root, "blob.bin", "\x00"+forbidden+" 5\x00")
	if blocks(t, root) {
		t.Error("expected pass — binary file skipped")
	}
}

func TestMatchCountReported(t *testing.T) {
	root := t.TempDir()
	w(t, root, "a.sh", "# allowlist: "+forbidden+"\necho \""+forbidden+"\"\necho \""+forbidden+"\"\n")
	o, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	// 3 matches: the marker line itself contains "migrate force" (same-line
	// permit), plus lines 2 and 3 (each within the 3-line lookback). Parity
	// with the bash gate's `wc -l < MATCHES_FILE` total.
	if o.MatchCount != 3 {
		t.Errorf("expected 3 matches, got %d", o.MatchCount)
	}
	if !o.Pass {
		t.Errorf("expected pass (all within marker lookback), got %v", o.Blocked)
	}
}

// A file the walk reaches and cannot read used to be skipped "for parity with
// grep", so the one unreadable file holding the command reported clean.
func TestUnreadableFileFailsTheScan(t *testing.T) {
	root := t.TempDir()
	w(t, root, "ok.sh", "echo ok\n")
	if err := os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(root, "dangling.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(Config{Root: root}); err == nil {
		t.Fatal("an unreadable file was skipped and the scan reported a verdict")
	}
}

// A line longer than the scanner buffer used to stop the scan silently, so a
// forbidden line after it was never read.
func TestOverlongLineFailsTheScan(t *testing.T) {
	root := t.TempDir()
	long := make([]byte, 5*1024*1024)
	for i := range long {
		long[i] = 'x'
	}
	w(t, root, "big.sh", string(long)+"\n"+"echo \""+forbidden+" 5\"\n")
	if _, err := Check(Config{Root: root}); err == nil {
		t.Fatal("an over-long line ended the scan and the file behind it went unread")
	}
}
