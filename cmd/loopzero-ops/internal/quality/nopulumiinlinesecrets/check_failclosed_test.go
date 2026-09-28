// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package nopulumiinlinesecrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheck_FailsClosedOnWalkError (FIX-2, M24): the gate must fail-closed
// (return a non-nil error) when it cannot read a scan dir, never swallow the
// walk error and report clean (fail-open blind spot). ADR-M24-3.
func TestCheck_FailsClosedOnWalkError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based walk-error test needs non-root (root bypasses chmod 000)")
	}
	root := t.TempDir()
	sub := filepath.Join(root, "infra/pulumi", "unreadable")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })

	if _, err := Check(Config{RepoRoot: root}); err == nil {
		t.Fatal("want fail-closed error on unreadable scan dir, got nil (fail-open regression)")
	}
}

// TestCheck_FailsClosedOnUnreadableFile: a stack file the walk reaches but
// cannot open must fail the gate, not become a warning beside a PASS. A dangling
// symlink is unreadable for every user, root included.
func TestCheck_FailsClosedOnUnreadableFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "infra/pulumi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(dir, "Pulumi.prod.yaml")); err != nil {
		t.Fatal(err)
	}

	out, err := Check(Config{RepoRoot: root})

	if err == nil {
		t.Fatalf("want fail-closed error on an unreadable stack file, got nil and Pass=%v", out.Pass)
	}
	if !strings.Contains(err.Error(), "Pulumi.prod.yaml") {
		t.Errorf("error %q does not name the file it could not read", err)
	}
}
