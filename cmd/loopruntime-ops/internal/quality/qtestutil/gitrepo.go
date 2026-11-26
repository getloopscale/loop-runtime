// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qtestutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// DeleteMarker as a mutation value removes the file in CommitOn.
const DeleteMarker = "<DELETE>"

// RenamePrefix is the mutation-value prefix that git-mv's the file in
// CommitOn to the destination path that follows it.
const RenamePrefix = "<RENAME>:"

// InitRepo builds a hermetic git repository in a temp dir with files
// committed on `main`, and returns its root.
func InitRepo(t testing.TB, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	MustRun(t, root, "git", "init", "-q", "--initial-branch=main")
	MustRun(t, root, "git", "config", "user.email", "test@loopruntime.local")
	MustRun(t, root, "git", "config", "user.name", "test")
	MustRun(t, root, "git", "config", "commit.gpgsign", "false")
	WriteFiles(t, root, files)
	MustRun(t, root, "git", "add", "-A")
	MustRun(t, root, "git", "commit", "-q", "-m", "initial")
	return root
}

// CommitOn creates branch and commits the mutations on it: DeleteMarker
// removes a path, RenamePrefix+dst moves it, any other value writes it.
func CommitOn(t testing.TB, root, branch string, mutations map[string]string) {
	t.Helper()
	MustRun(t, root, "git", "switch", "-q", "-c", branch)
	for rel, content := range mutations {
		switch {
		case content == DeleteMarker:
			MustRun(t, root, "git", "rm", "-q", rel)
		case strings.HasPrefix(content, RenamePrefix):
			dst := strings.TrimPrefix(content, RenamePrefix)
			if err := os.MkdirAll(filepath.Join(root, filepath.Dir(dst)), 0o750); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			MustRun(t, root, "git", "mv", rel, dst)
		default:
			WriteFiles(t, root, map[string]string{rel: content})
		}
	}
	MustRun(t, root, "git", "add", "-A")
	MustRun(t, root, "git", "commit", "-q", "-m", "feature change")
}

// WriteFiles writes repo-relative files under root, creating directories.
func WriteFiles(t testing.TB, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

// MustRun runs a command in dir and fails the test on error.
func MustRun(t testing.TB, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
