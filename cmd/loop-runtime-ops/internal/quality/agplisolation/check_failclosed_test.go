// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package agplisolation

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCheck_FailsClosedOnWalkError (M25): a walk error means the gate could not
// read files it must scan; it must fail-closed (Pass=false), never report clean.
func TestCheck_FailsClosedOnWalkError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based walk-error test needs non-root")
	}
	root := t.TempDir()
	sub := filepath.Join(root, "api", "unreadable")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })

	out := Check(Config{Root: root})
	if out.Pass {
		t.Fatal("want Pass=false (fail-closed) on unreadable scan dir")
	}
}
