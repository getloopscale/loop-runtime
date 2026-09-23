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
	root := repo(t, map[string]string{"scripts/locked.sh": "echo hi\n"})
	p := filepath.Join(root, "scripts", "locked.sh")
	// A dangling symlink is unreadable for every uid, root included.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(filepath.Dir(p), "does-not-exist"), p); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(Config{Root: root}); err == nil {
		t.Fatal("want an error for an unreadable file in scope, got nil")
	}
}
