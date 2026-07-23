// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package nolegacysatoken

import (
	"os"
	"path/filepath"
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
