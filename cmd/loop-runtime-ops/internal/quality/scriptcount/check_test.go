// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scriptcount

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitCmd runs a git subcommand in root and fails the test on error.
func gitCmd(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// initRepo turns root into a git repo. The count source is the index, so no
// commit is needed — `git add` is enough for `git ls-files` to see a file.
func initRepo(t *testing.T, root string) {
	t.Helper()
	gitCmd(t, root, "init", "-q")
}

// mkscript creates an empty .sh at root/rel (creating parent dirs) — on disk only.
func mkscript(t *testing.T, root, rel string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/usr/bin/env bash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// track creates a .sh AND stages it (so git ls-files lists it).
func track(t *testing.T, root, rel string) {
	t.Helper()
	mkscript(t, root, rel)
	gitCmd(t, root, "add", "--", rel)
}

func TestScriptCount_EqualsBaseline_Pass(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	track(t, root, "infra/scripts/a.sh")
	track(t, root, "infra/scripts/b.sh")
	v, err := Check(root, Baseline{CanonicalSH: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !v.OK || v.Count != 2 {
		t.Fatalf("want OK count=2, got OK=%v count=%d", v.OK, v.Count)
	}
}

func TestScriptCount_BelowBaseline_Pass(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	track(t, root, "infra/scripts/a.sh")
	v, _ := Check(root, Baseline{CanonicalSH: 2})
	if !v.OK || !v.Improved || v.Count != 1 {
		t.Fatalf("want OK+Improved count=1, got OK=%v Improved=%v count=%d", v.OK, v.Improved, v.Count)
	}
}

func TestScriptCount_AboveBaseline_Fail(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	track(t, root, "infra/scripts/a.sh")
	track(t, root, "infra/scripts/b.sh")
	track(t, root, "infra/scripts/c.sh")
	v, _ := Check(root, Baseline{CanonicalSH: 2})
	if v.OK || v.Count != 3 {
		t.Fatalf("want FAIL count=3, got OK=%v count=%d", v.OK, v.Count)
	}
}

// Issue #151 anchor: an untracked .sh physically present on disk (e.g. a
// gitignored discover reference clone, a dist/build artifact, a tool cache)
// MUST NOT count. This is the determinism property — the same commit yields the
// same count regardless of what junk sits in the working tree.
func TestScriptCount_IgnoresUntrackedOnDisk(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	track(t, root, "infra/scripts/a.sh")
	mkscript(t, root, "some/generated/dir/leftover.sh") // on disk, NOT staged
	v, _ := Check(root, Baseline{CanonicalSH: 1})
	if !v.OK || v.Count != 1 {
		t.Fatalf("untracked on-disk script counted: count=%d (want 1) — #151 regression", v.Count)
	}
}

// A gitignored path is never in the index, so it is never counted — even when
// physically present. Mirrors the discover reference-clone scenario.
func TestScriptCount_IgnoresGitignored(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	track(t, root, "infra/scripts/a.sh")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"),
		[]byte(".claude/knowledge-base/references/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkscript(t, root, ".claude/knowledge-base/references/cert-manager/hack/build.sh") // gitignored, on disk
	v, _ := Check(root, Baseline{CanonicalSH: 1})
	if !v.OK || v.Count != 1 {
		t.Fatalf("gitignored reference-clone script counted: count=%d (want 1)", v.Count)
	}
}

func TestScriptCount_ExcludesWorktrees(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	track(t, root, "infra/scripts/a.sh")
	// Force-staged under the worktrees segment: the secondary filter must drop it
	// even though it is in the index.
	track(t, root, ".claude/worktrees/foo/infra/scripts/dup.sh")
	v, _ := Check(root, Baseline{CanonicalSH: 1})
	if !v.OK || v.Count != 1 {
		t.Fatalf("worktree script counted: count=%d (want 1)", v.Count)
	}
}

// EC-1: a script git-mv'd to trash/ during a migration is still tracked (in the
// index), so git ls-files lists it. The gate MUST exclude trash/ via the
// secondary filter so the verdict is stable through the trash→delete window.
func TestScriptCount_ExcludesTrash(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	track(t, root, "infra/scripts/a.sh")
	track(t, root, "trash/infra-scripts/moved.sh") // tracked but must NOT count (EC-1)
	v, _ := Check(root, Baseline{CanonicalSH: 1})
	if !v.OK || v.Count != 1 {
		t.Fatalf("trash script counted: count=%d (want 1) — EC-1 regression", v.Count)
	}
}

// Defense in depth: even a force-added (`git add -f`) reference-clone script —
// which bypasses .gitignore and lands in the index — is dropped by the
// secondary segment filter.
func TestScriptCount_ExcludesForceAddedReferenceClone(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	track(t, root, "infra/scripts/a.sh")
	mkscript(t, root, ".claude/knowledge-base/references/cert-manager/hack/build.sh")
	gitCmd(t, root, "add", "-f", "--", ".claude/knowledge-base/references/cert-manager/hack/build.sh")
	v, _ := Check(root, Baseline{CanonicalSH: 1})
	if !v.OK || v.Count != 1 {
		t.Fatalf("force-added reference-clone script counted: count=%d (want 1)", v.Count)
	}
}

func TestParseBaseline(t *testing.T) {
	b, err := ParseBaseline([]byte(`{"canonical_sh":140}`))
	if err != nil || b.CanonicalSH != 140 {
		t.Fatalf("parse: got %d err=%v", b.CanonicalSH, err)
	}
}
