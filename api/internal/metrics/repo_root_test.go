// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// B-178 — a guard that was red only in the environment people work in.
//
// findRepoRoot locates the repo root for the sigstore observability mirror
// tests. It used to require `.git` to be a DIRECTORY. That is true in an
// ordinary clone and false in a linked worktree, where `git worktree add`
// writes `.git` as a regular file containing a `gitdir:` pointer. The walk
// therefore climbed past the real root to `/` and failed, so
// TestSigstoreMetrics_AllDeclaredAreVisualized, _DashboardRefsAreDeclared and
// _AllDeclaredHaveWrapper failed in every linked worktree and passed in every
// ordinary checkout.
//
// The environment measurement is the real regression proof, and it cannot be
// written as a Go test: it is "run the suite from a worktree". These cases pin
// the discovery itself instead — both `.git` shapes, hermetically, in a
// temp dir — so the constraint is asserted somewhere a reader can find it
// rather than living only in the item's evidence.

package metrics

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFindRepoRoot_AcceptsGitAsFileAndAsDirectory is the B-178 regression
// guard. The `git file` case is the one that was broken; the `git dir` case is
// asserted alongside it because a fix that traded one environment for the
// other would be the same defect facing the other way.
func TestFindRepoRoot_AcceptsGitAsFileAndAsDirectory(t *testing.T) {
	// newRepo builds a plausible root — `.git` in the requested shape plus the
	// go.work the discovery also requires — with a nested package dir to
	// walk up from, mirroring api/internal/metrics.
	newRepo := func(t *testing.T, gitIsDir bool) (root, nested string) {
		t.Helper()
		root = t.TempDir()
		gitPath := filepath.Join(root, ".git")
		if gitIsDir {
			if err := os.Mkdir(gitPath, 0o755); err != nil {
				t.Fatalf("mkdir .git: %v", err)
			}
		} else {
			// The shape `git worktree add` writes: a single `gitdir:` line. The path
			// inside it is a placeholder on purpose — findRepoRoot checks that .git
			// EXISTS and never reads it, so a real absolute path would add nothing
			// here except a workstation-path leak, which `publish-hygiene` refuses.
			if err := os.WriteFile(gitPath, []byte("gitdir: <repo>/.git/worktrees/lane\n"), 0o644); err != nil {
				t.Fatalf("write .git file: %v", err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.26.11\n"), 0o644); err != nil {
			t.Fatalf("write go.work: %v", err)
		}
		nested = filepath.Join(root, "api", "internal", "metrics")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatalf("mkdir nested: %v", err)
		}
		return root, nested
	}

	t.Run("git is a file (linked worktree)", func(t *testing.T) {
		root, nested := newRepo(t, false)
		got, err := findRepoRoot(nested)
		if err != nil {
			t.Fatalf("findRepoRoot returned %v — B-178: in a linked worktree `.git` is a regular file holding a gitdir: pointer, and requiring it to be a directory makes the walk climb to / and fail. Every lane works in a worktree, so this is the environment the guard has to work in", err)
		}
		if got != root {
			t.Errorf("findRepoRoot = %q, want %q", got, root)
		}
	})

	t.Run("git is a directory (ordinary checkout)", func(t *testing.T) {
		root, nested := newRepo(t, true)
		got, err := findRepoRoot(nested)
		if err != nil {
			t.Fatalf("findRepoRoot returned %v — the ordinary clone must keep working; a fix that only moved the failure to the other environment is the same defect", err)
		}
		if got != root {
			t.Errorf("findRepoRoot = %q, want %q", got, root)
		}
	})

	// The go.work half of the marker is load-bearing now that `.git` is
	// only checked for existence: without it, any stray `.git` above the tree
	// would stop the walk early and the tests would read the wrong files.
	t.Run("a bare .git without go.work is not a root", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
			t.Fatalf("write .git file: %v", err)
		}
		nested := filepath.Join(root, "a", "b")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatalf("mkdir nested: %v", err)
		}
		if got, err := findRepoRoot(nested); err == nil {
			t.Errorf("findRepoRoot = %q with no go.work present, want an error — relaxing the .git check must not relax the whole marker", got)
		}
	})
}
