// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package linescan

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLines_NumbersLinesWithoutATrailingEmptyOne(t *testing.T) {
	got, err := Lines([]byte("a\nb\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %q, want [a b]", got)
	}
}

// A line past the buffer used to end the scan silently: every line after it was
// never read and the file reported clean.
func TestLines_RefusesALineLongerThanTheBuffer(t *testing.T) {
	long := strings.Repeat("x", maxLine+1)
	if _, err := Lines([]byte("ok\n" + long + "\nforbidden\n")); err == nil {
		t.Fatal("an over-long line was accepted, so the lines after it were never read")
	}
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWalk_VisitsKeptFilesWithRepoRelativePaths(t *testing.T) {
	root := t.TempDir()
	write(t, root, "charts/a/t.yaml", "x\n")
	write(t, root, "charts/a/README.md", "x\n")
	var seen []string
	w, err := Walk(root, []string{"charts"}, func(rel string) bool { return strings.HasSuffix(rel, ".yaml") },
		func(rel string, _ []byte) error { seen = append(seen, rel); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "charts/a/t.yaml" || w.Files != 1 || w.Dirs != 1 {
		t.Fatalf("seen=%v walked=%+v", seen, w)
	}
}

// Every root absent is a scan over nothing: the caller must be able to tell.
func TestWalk_CountsOnlyTheDirectoriesThatExist(t *testing.T) {
	w, err := Walk(t.TempDir(), []string{"gone", "also-gone"}, func(string) bool { return true },
		func(string, []byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if w.Dirs != 0 || len(w.Missing) != 2 {
		t.Fatalf("absent roots were not reported: %+v", w)
	}
}

// A file the walk reaches and cannot read must fail the walk, never be skipped.
func TestWalk_UnreadableFileIsAnError(t *testing.T) {
	root := t.TempDir()
	write(t, root, "charts/ok.yaml", "x\n")
	if err := os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(root, "charts", "dangling.yaml")); err != nil {
		t.Fatal(err)
	}
	_, err := Walk(root, []string{"charts"}, func(string) bool { return true },
		func(string, []byte) error { return nil })
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("an unreadable file did not fail the walk: %v", err)
	}
}

func TestWalk_VisitErrorStopsTheWalk(t *testing.T) {
	root := t.TempDir()
	write(t, root, "charts/a.yaml", "x\n")
	sentinel := errors.New("boom")
	_, err := Walk(root, []string{"charts"}, func(string) bool { return true },
		func(string, []byte) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("a visit error was lost: %v", err)
	}
}

func TestWalk_PrunesSkippedDirectories(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/node_modules/x.js", "x\n")
	write(t, root, "src/a.js", "x\n")
	var seen []string
	_, err := WalkPruned(root, []string{"src"}, map[string]bool{"node_modules": true}, func(string) bool { return true },
		func(rel string, _ []byte) error { seen = append(seen, rel); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "src/a.js" {
		t.Fatalf("pruned directory was walked: %v", seen)
	}
}

// WalkDir never descends into a symlinked directory, so neither does Walk; the
// link itself is not a file to read.
func TestWalk_SymlinkToADirectoryIsNotAFile(t *testing.T) {
	root := t.TempDir()
	write(t, root, "elsewhere/x.yaml", "x\n")
	write(t, root, "charts/a.yaml", "x\n")
	if err := os.Symlink(filepath.Join(root, "elsewhere"), filepath.Join(root, "charts", "linked")); err != nil {
		t.Fatal(err)
	}
	w, err := Walk(root, []string{"charts"}, func(string) bool { return true },
		func(string, []byte) error { return nil })
	if err != nil {
		t.Fatalf("a symlinked directory failed the walk: %v", err)
	}
	if w.Files != 1 {
		t.Fatalf("files = %d, want 1", w.Files)
	}
}
