// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package revrange

import (
	"errors"
	"os/exec"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "root"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestVerifyReachable_AcceptsACommit(t *testing.T) {
	if err := VerifyReachable(initRepo(t), "HEAD"); err != nil {
		t.Fatalf("HEAD must be reachable: %v", err)
	}
}

func TestVerifyReachable_RefusesAnUnknownRef(t *testing.T) {
	err := VerifyReachable(initRepo(t), "origin/does-not-exist")

	if !errors.Is(err, ErrBaseRefUnreachable) {
		t.Fatalf("err = %v, want ErrBaseRefUnreachable", err)
	}
}

func TestVerifyReachable_RefusesADirectoryThatIsNotARepository(t *testing.T) {
	err := VerifyReachable(t.TempDir(), "HEAD")

	if !errors.Is(err, ErrBaseRefUnreachable) {
		t.Fatalf("outside a git repository no ref resolves; err = %v, want ErrBaseRefUnreachable", err)
	}
}
