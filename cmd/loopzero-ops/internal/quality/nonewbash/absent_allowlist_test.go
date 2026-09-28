// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package nonewbash

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "--initial-branch=main"},
		{"config", "user.email", "test@loopzero.local"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "seed"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return root
}

// An ABSENT allowlist declares no exemptions; it is not an infrastructure failure.
//
// The distinction decides whether this gate can run at all. Its allowlist lived at
// `infra/quality/shell-scripts-allowlist.md`, and `3d4075ca3` (2026-09-08) removed
// the whole installed tooling tree from tracking — so the file exists in no clone,
// on no runner, and for no contributor. Measured 2026-09-16: the gate exits 5 with
// "read allowlist ...: no such file or directory", which blocked this repository's
// own pre-push hook and would have been a guaranteed red on the first pull request.
//
// Absence reads as "nothing is exempt", which is the STRICTEST answer available and
// the one that matches what this gate is for: refusing new shell scripts. A gate
// whose default exempts nothing cannot be weakened by losing its file.
func TestCheck_AbsentAllowlistDeclaresNoExemptions(t *testing.T) {
	root := initRepo(t)

	out, err := Check(Config{
		RepoRoot:      root,
		AllowlistPath: ".this-file-does-not-exist.md",
		BaseRef:       "HEAD",
		HeadRef:       "HEAD",
	})
	if err != nil {
		t.Fatalf("an absent allowlist must not be an infrastructure failure, got: %v", err)
	}
	if out == nil {
		t.Fatal("expected an outcome, got nil")
	}
	if !out.Pass {
		t.Errorf("no diff and no exemptions needed: expected Pass, got violations %v", out.Violations)
	}
}

// The other half of the same contract: present-but-unreadable is still an error,
// because that is a different claim. The file is there and something is wrong with
// reading it, and calling that "no exemptions" would discard exemptions somebody
// wrote — which is the failure the fatal default was protecting against.
func TestCheck_UnreadableAllowlistIsStillAnError(t *testing.T) {
	root := initRepo(t)

	// A DIRECTORY at the allowlist path: it exists, and it cannot be read as a file.
	if err := os.MkdirAll(filepath.Join(root, "allowlist.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Check(Config{
		RepoRoot:      root,
		AllowlistPath: "allowlist.md",
		BaseRef:       "HEAD",
		HeadRef:       "HEAD",
	})
	if err == nil {
		t.Fatal("an unreadable allowlist is an infrastructure failure and must error")
	}
	if !strings.Contains(err.Error(), "read allowlist") {
		t.Errorf("error should name the allowlist read, got: %v", err)
	}
}
