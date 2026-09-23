// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package drilloutputs

import (
	"os"
	"path/filepath"
	"testing"
)

// An unreadable file in scope was never scanned; it is not a clean file.
func TestUnreadableFileFailsInsteadOfBeingSkipped(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based read-error test needs non-root (root bypasses chmod 000)")
	}
	root := repo(t, map[string]string{"scripts/locked.sh": "echo hi\n"})
	p := filepath.Join(root, "scripts", "locked.sh")
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	if _, err := Check(Config{Root: root}); err == nil {
		t.Fatal("want an error for an unreadable file in scope, got nil")
	}
}
