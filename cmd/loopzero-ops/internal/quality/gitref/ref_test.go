// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitref

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"commit", "-q", "--allow-empty", "-m", "seed"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return root
}

func TestVerifyRefReachable_ExistingRefIsReachable(t *testing.T) {
	if err := VerifyRefReachable(initRepo(t), "main"); err != nil {
		t.Fatalf("want nil for an existing ref, got %v", err)
	}
}

func TestVerifyRefReachable_UnknownRefNamesTheFailureClass(t *testing.T) {
	err := VerifyRefReachable(initRepo(t), "origin/does-not-exist")
	if err == nil || !strings.HasPrefix(err.Error(), "BASE_REF_UNREACHABLE") {
		t.Fatalf("want a BASE_REF_UNREACHABLE error, got %v", err)
	}
}

func TestVerifyRefReachable_NonRepositoryIsUnreachable(t *testing.T) {
	if err := VerifyRefReachable(t.TempDir(), "HEAD"); err == nil {
		t.Fatal("want an error outside a git repository")
	}
}
