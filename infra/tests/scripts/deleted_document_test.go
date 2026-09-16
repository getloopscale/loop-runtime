// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// readDocument returns a document's bytes from the worktree, falling back to git history.
//
// A test whose subject is a DOCUMENT is asserting what that document says. Two commits in
// this repository moved a great many documents — `19b8c6eaa` folded 1,523 files into six
// living ones, and `3d4075ca3` removed the installed kit — and a document that left the
// worktree in one of them has not stopped saying what it said. It is in history exactly,
// forever, which is the one location a consolidation cannot take away.
//
// Disk is tried first on purpose: a document that came back is better evidence than one
// only in history, and a test that reads history when the live file exists would be
// asserting about the past while the present disagreed.
//
// Failing loudly when NEITHER answers is the point. This is a fallback for a document that
// moved, not a way for a test to stop having a subject.
func readDocument(t *testing.T, rev string, rel ...string) string {
	t.Helper()
	relPath := filepath.Join(rel...)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(root, relPath)); err == nil {
		return string(b)
	}
	cmd := exec.Command("git", "show", rev+":"+relPath) // #nosec G204 -- rev and path are constants at each call site
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s is absent from the worktree and unreadable at %s: %v", relPath, rev, err)
	}
	return string(out)
}
