// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// readDocument returns a document's bytes from the worktree, falling back to git history.
//
// A test whose subject is a DOCUMENT is asserting what that document says. Two commits in
// this repository moved a great many documents (one folded 1,523 files into six living
// ones, another removed the installed kit), and a document that left the
// worktree in one of them has not stopped saying what it said. It is in history exactly,
// forever, which is the one location a consolidation cannot take away.
//
// Disk is tried first on purpose: a document that came back is better evidence than one
// only in history, and a test that reads history when the live file exists would be
// asserting about the past while the present disagreed.
//
// The revision is the parent of the commit that deleted the file, found by asking git
// rather than written down: the history was rewritten once (the rename to LoopZero), and
// every hash a test had pinned stopped existing.
//
// Failing loudly when NEITHER answers is the point. This is a fallback for a document that
// moved, not a way for a test to stop having a subject.
func readDocument(t *testing.T, rel ...string) string {
	t.Helper()
	relPath := filepath.Join(rel...)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(root, relPath)); err == nil {
		return string(b)
	}
	git := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...) // #nosec G204 -- the path is a constant at each call site
		cmd.Dir = root
		out, err := cmd.Output()
		return string(out), err
	}
	deletedBy, err := git("log", "--diff-filter=D", "--format=%H", "-1", "--", relPath)
	deletedBy = strings.TrimSpace(deletedBy)
	if err != nil || deletedBy == "" {
		t.Fatalf("%s is absent from the worktree and no commit in history deleted it (%v)", relPath, err)
	}
	out, err := git("show", deletedBy+"~1:"+relPath)
	if err != nil {
		t.Fatalf("%s is absent from the worktree and unreadable before %s deleted it: %v", relPath, deletedBy, err)
	}
	return out
}
