// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package canonicalfixturerepo

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCheck_FailsClosedOnWalkError (M25): the gate must fail-closed (return a
// non-nil error) when it cannot read a scan dir, never swallow the walk error.
func TestCheck_FailsClosedOnWalkError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based walk-error test needs non-root")
	}
	root := t.TempDir()
	sub := filepath.Join(root, "infra/tests/e2e", "unreadable")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })

	if _, err := violationOf(Check(Config{RepoRoot: root})); err == nil {
		t.Fatal("want fail-closed error on unreadable scan dir, got nil")
	}
}
