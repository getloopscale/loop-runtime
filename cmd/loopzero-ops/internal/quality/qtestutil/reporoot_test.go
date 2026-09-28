// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qtestutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoot_ReturnsTheDirectoryHoldingGoWork(t *testing.T) {
	root := Root(t)

	if _, err := os.Stat(filepath.Join(root, "go.work")); err != nil {
		t.Fatalf("Root() = %s, which holds no go.work: %v", root, err)
	}
}

func TestRootFile_ReturnsTheRootWhenTheSubjectExists(t *testing.T) {
	root := RootFile(t, "go.work")

	if root != Root(t) {
		t.Fatalf("RootFile() = %s, want %s", root, Root(t))
	}
}

func TestRepoRoot_FailsOutsideAnyWorkspace(t *testing.T) {
	t.Chdir(t.TempDir())

	if _, err := RepoRoot(); err == nil {
		t.Fatal("RepoRoot() succeeded in a directory tree without go.work")
	}
}
