// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scriptcount

import (
	"os"
	"path/filepath"
	"testing"
)

// mkscript creates an empty .sh at root/rel (creating parent dirs).
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

func TestScriptCount_EqualsBaseline_Pass(t *testing.T) {
	root := t.TempDir()
	mkscript(t, root, "infra/scripts/a.sh")
	mkscript(t, root, "infra/scripts/b.sh")
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
	mkscript(t, root, "infra/scripts/a.sh")
	v, _ := Check(root, Baseline{CanonicalSH: 2})
	if !v.OK || !v.Improved || v.Count != 1 {
		t.Fatalf("want OK+Improved count=1, got OK=%v Improved=%v count=%d", v.OK, v.Improved, v.Count)
	}
}

func TestScriptCount_AboveBaseline_Fail(t *testing.T) {
	root := t.TempDir()
	mkscript(t, root, "infra/scripts/a.sh")
	mkscript(t, root, "infra/scripts/b.sh")
	mkscript(t, root, "infra/scripts/c.sh")
	v, _ := Check(root, Baseline{CanonicalSH: 2})
	if v.OK || v.Count != 3 {
		t.Fatalf("want FAIL count=3, got OK=%v count=%d", v.OK, v.Count)
	}
}

func TestScriptCount_ExcludesWorktrees(t *testing.T) {
	root := t.TempDir()
	mkscript(t, root, "infra/scripts/a.sh")
	mkscript(t, root, ".claude/worktrees/foo/infra/scripts/dup.sh") // must NOT count
	v, _ := Check(root, Baseline{CanonicalSH: 1})
	if !v.OK || v.Count != 1 {
		t.Fatalf("worktree script counted: count=%d (want 1)", v.Count)
	}
}

// EC-1: a script git-mv'd to trash/ during a migration is still tracked, so a
// naive find would count it in the trash→delete window. The gate MUST exclude
// trash/ so the verdict is stable through the migration window.
func TestScriptCount_ExcludesTrash(t *testing.T) {
	root := t.TempDir()
	mkscript(t, root, "infra/scripts/a.sh")
	mkscript(t, root, "trash/infra-scripts/moved.sh") // must NOT count (EC-1)
	v, _ := Check(root, Baseline{CanonicalSH: 1})
	if !v.OK || v.Count != 1 {
		t.Fatalf("trash script counted: count=%d (want 1) — EC-1 regression", v.Count)
	}
}

// Third-party reference clones live under .claude/knowledge-base/references/
// (gitignored study material from discover cycles — cert-manager, cloudnative-pg,
// crossplane, etc.). They carry hundreds of upstream .sh files that are NOT part
// of LoopZero's script-minimization program. Counting them makes the gate
// non-deterministic (depends on which references happen to be cloned on disk) —
// the same double-count pathology that justifies the worktrees/ exclusion.
func TestScriptCount_ExcludesReferenceClones(t *testing.T) {
	root := t.TempDir()
	mkscript(t, root, "infra/scripts/a.sh")
	mkscript(t, root, ".claude/knowledge-base/references/cert-manager/hack/build.sh") // must NOT count
	v, _ := Check(root, Baseline{CanonicalSH: 1})
	if !v.OK || v.Count != 1 {
		t.Fatalf("reference-clone script counted: count=%d (want 1)", v.Count)
	}
}

func TestParseBaseline(t *testing.T) {
	b, err := ParseBaseline([]byte(`{"canonical_sh":140}`))
	if err != nil || b.CanonicalSH != 140 {
		t.Fatalf("parse: got %d err=%v", b.CanonicalSH, err)
	}
}
