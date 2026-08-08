// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package decisionsdeletion

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitRepo builds a throwaway repo: seeds `file` (with content) on main, then on
// a `pr` branch deletes it and optionally adds a superseder file. Returns root.
func gitRepo(t *testing.T, file, content, superseder, superContent string) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	run("init", "-q")
	run("checkout", "-q", "-b", "main")
	write(file, content)
	run("add", file)
	run("commit", "-q", "-m", "seed")

	run("checkout", "-q", "-b", "pr")
	run("rm", "-q", file)
	if superseder != "" {
		write(superseder, superContent)
		run("add", superseder)
	}
	run("commit", "-q", "-m", "delete protected file")
	return root
}

func check(t *testing.T, root string) *Outcome {
	t.Helper()
	return Check(Config{RepoRoot: root, BaseRef: "main", HeadRef: "pr"})
}

// Scenario 1 — delete ADR without superseder → violation.
func TestDeleteDecisionNoSuperseder(t *testing.T) {
	root := gitRepo(t, "docs/wiki/decisions/2026-05-12-test.md",
		"---\ntype: decision\n---\nbody", "", "")
	if check(t, root).Pass {
		t.Error("expected violation")
	}
}

// Scenario 2 — delete ADR with superseder → clean.
func TestDeleteDecisionWithSuperseder(t *testing.T) {
	root := gitRepo(t,
		"docs/wiki/decisions/2026-05-12-test.md",
		"---\ntype: decision\n---\nbody",
		"docs/wiki/decisions/2026-05-12-replacement.md",
		"---\ntype: decision\nsuperseded_by: 2026-05-12-test.md\n---\nbody")
	o := check(t, root)
	if !o.Pass {
		t.Errorf("expected clean, violations=%v", o.Violations)
	}
	if len(o.Superseded) != 1 {
		t.Errorf("expected 1 superseded record, got %v", o.Superseded)
	}
}

// Scenario 3 — delete MEET DR without superseder → violation.
func TestDeleteMeetingNoSuperseder(t *testing.T) {
	root := gitRepo(t, "docs/wiki/meetings/MEET-2026-05-12-test.md",
		"---\ntype: meeting\n---\nbody", "", "")
	if check(t, root).Pass {
		t.Error("expected violation")
	}
}

// Scenario 4 — delete operations runbook without superseder → violation.
func TestDeleteRunbookNoSuperseder(t *testing.T) {
	root := gitRepo(t,
		"docs/wiki/concepts/operations/network-policy-egress-runbook.md",
		"---\ntype: reference\n---\nbody", "", "")
	if check(t, root).Pass {
		t.Error("expected violation for runbook")
	}
}

// Scenario 5 — delete operations inventory without superseder → violation.
func TestDeleteInventoryNoSuperseder(t *testing.T) {
	root := gitRepo(t,
		"docs/wiki/concepts/operations/network-policy-egress-inventory.md",
		"---\ntype: reference\n---\nbody", "", "")
	if check(t, root).Pass {
		t.Error("expected violation for inventory")
	}
}

// Scenario 6 — delete arbitrary operations note (NOT runbook/inventory) → clean.
func TestDeleteArbitraryOperationsNote(t *testing.T) {
	root := gitRepo(t,
		"docs/wiki/concepts/operations/random-notes.md",
		"---\ntype: reference\n---\nbody", "", "")
	o := check(t, root)
	if !o.Pass {
		t.Errorf("arbitrary note must NOT be protected, violations=%v", o.Violations)
	}
	if len(o.DeletedFiles) != 0 {
		t.Errorf("arbitrary note must not match glob, got %v", o.DeletedFiles)
	}
}

// Scenario 7 — delete inventory WITH superseder → clean.
func TestDeleteInventoryWithSuperseder(t *testing.T) {
	root := gitRepo(t,
		"docs/wiki/concepts/operations/old-inventory.md",
		"---\ntype: reference\n---\nbody",
		"docs/wiki/concepts/operations/new-inventory.md",
		"---\ntype: reference\nsuperseded_by: old-inventory.md\n---\nbody")
	if !check(t, root).Pass {
		t.Error("expected clean with superseder")
	}
}

// Scenario 8 — no protected deletions → clean.
func TestNoProtectedDeletions(t *testing.T) {
	root := gitRepo(t, "src/code.go", "package main", "", "")
	o := check(t, root)
	if !o.Pass || len(o.DeletedFiles) != 0 {
		t.Errorf("non-protected deletion should be clean, got %+v", o)
	}
}

// Extra — non-.md siblings in the protected dir are skipped while searching
// for a superseder (exercises the .md filter branch).
func TestNonMarkdownSiblingSkipped(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	dir := "docs/wiki/decisions"
	run("init", "-q")
	run("checkout", "-q", "-b", "main")
	write(dir+"/2026-05-12-old.md", "---\ntype: decision\n---\nbody")
	run("add", "-A")
	run("commit", "-q", "-m", "seed")
	run("checkout", "-q", "-b", "pr")
	run("rm", "-q", dir+"/2026-05-12-old.md")
	write(dir+"/notes.txt", "a non-markdown sibling that must be skipped")
	write(dir+"/2026-05-12-new.md", "---\nsuperseded_by: 2026-05-12-old.md\n---\nbody")
	run("add", "-A")
	run("commit", "-q", "-m", "delete + supersede")

	o := check(t, root)
	if !o.Pass {
		t.Errorf("expected clean (superseder present despite .txt sibling), got %+v", o)
	}
}

// Extra — a non-git directory yields no deletions (git fails gracefully → clean).
func TestNonGitDirIsClean(t *testing.T) {
	root := t.TempDir() // no .git
	o := Check(Config{RepoRoot: root, BaseRef: "main", HeadRef: "pr"})
	if !o.Pass || len(o.DeletedFiles) != 0 {
		t.Errorf("non-git dir should be clean, got %+v", o)
	}
}

// Extra — empty refs apply defaults (origin/main..HEAD); unknown base fails
// gracefully to a clean result.
func TestDefaultRefsApplied(t *testing.T) {
	root := gitRepo(t, "src/code.go", "package main", "", "")
	o := Check(Config{RepoRoot: root}) // empty refs → origin/main...HEAD
	if !o.Pass {
		t.Errorf("expected clean with default refs, got %+v", o)
	}
}

// Scenario 10 — a rename is not a deletion. The artifact still exists under a
// new name, so demanding a superseding record for it is a false positive.
func TestRenamedProtectedFileIsNotDeletion(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	dir := "docs/wiki/decisions"
	if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "---\ntype: decision\n---\nthe body must be long enough for git to score the rename as a rename\n"
	if err := os.WriteFile(filepath.Join(root, dir, "2026-05-12-old.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("init", "-q")
	run("checkout", "-q", "-b", "main")
	run("add", "-A")
	run("commit", "-q", "-m", "seed")

	run("checkout", "-q", "-b", "pr")
	run("mv", dir+"/2026-05-12-old.md", dir+"/2026-05-12-renamed.md")
	run("commit", "-q", "-m", "rename decision")

	o := check(t, root)
	if !o.Pass {
		t.Errorf("a rename is not a deletion, violations=%v", o.Violations)
	}
	if len(o.DeletedFiles) != 0 {
		t.Errorf("renamed file must not count as deleted, got %v", o.DeletedFiles)
	}
}

// Extra — YAML-list superseder form (`  - <basename>`) is also accepted.
func TestSupersederYAMLListForm(t *testing.T) {
	root := gitRepo(t,
		"docs/wiki/decisions/2026-05-12-old.md",
		"---\ntype: decision\n---\nbody",
		"docs/wiki/decisions/2026-05-12-new.md",
		"---\ntype: decision\nsupersedes:\n  - 2026-05-12-old.md\n---\nbody")
	if !check(t, root).Pass {
		t.Error("expected clean with YAML-list superseder form")
	}
}
