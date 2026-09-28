// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package nolegacysatoken

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheck_FailsClosedOnWalkError (FIX-2, M24): a security gate that cannot read
// the files it is meant to scan MUST fail-closed (return an error), never swallow
// the walk error and report Pass=true (fail-open blind spot). ADR-M24-3.
func TestCheck_FailsClosedOnWalkError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based walk-error test needs non-root (root bypasses chmod 000)")
	}
	root := t.TempDir()
	sub := filepath.Join(root, "charts", "unreadable")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) }) // restore so TempDir cleanup works

	_, err := Check(Config{RepoRoot: root})

	if err == nil {
		t.Fatal("want fail-closed error on unreadable scan dir, got nil (fail-open regression)")
	}
}

// TestCheck_FailsClosedOnUnreadableFile: the walk reaching a chart file it then
// cannot open is the same blind spot one level down — the file is counted as
// scanned by nothing, so Pass must not be true over it. A dangling symlink is an
// unreadable file for every user, root included, so the test needs no skip.
func TestCheck_FailsClosedOnUnreadableFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "charts", "svc", "templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(dir, "token.yaml")); err != nil {
		t.Fatal(err)
	}

	out, err := Check(Config{RepoRoot: root})

	if err == nil {
		t.Fatalf("want fail-closed error on an unreadable chart file, got nil and Pass=%v", out.Pass)
	}
	if !strings.Contains(err.Error(), "token.yaml") {
		t.Errorf("error %q does not name the file it could not read", err)
	}
}
