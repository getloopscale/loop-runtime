// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package decisionsdeletion

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repo is a throwaway git repository with a `main` base branch and a `pr`
// head branch.
type repo struct {
	t    *testing.T
	root string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	r := &repo{t: t, root: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	return r
}

func (r *repo) git(args ...string) {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (r *repo) write(rel, body string) {
	r.t.Helper()
	p := filepath.Join(r.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		r.t.Fatalf("write: %v", err)
	}
}

func (r *repo) commit(msg string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "--allow-empty", "-m", msg)
}

// seeded returns a repo whose base holds every protected document, already
// switched to the `pr` branch.
func seeded(t *testing.T) *repo {
	t.Helper()
	r := newRepo(t)
	for _, p := range ProtectedDocs {
		r.write(p, "# "+p+"\n\nlong enough body for git to score renames as renames\n")
	}
	r.commit("seed")
	r.git("switch", "-q", "-c", "pr")
	return r
}

func (r *repo) check() (*Outcome, error) {
	return Check(Config{RepoRoot: r.root, BaseRef: "main", HeadRef: "pr"})
}

func TestProtectedDocs_IncludesTheLivingDecisionRecord(t *testing.T) {
	for _, p := range ProtectedDocs {
		if p == "docs/decisions.md" {
			return
		}
	}
	t.Fatalf("docs/decisions.md is the living ADR document and must be protected; got %v", ProtectedDocs)
}

func TestDeletingDecisionsWithoutSuperseder_IsAViolation(t *testing.T) {
	r := seeded(t)
	r.git("rm", "-q", "docs/decisions.md")
	r.commit("delete")
	o, err := r.check()
	if err != nil {
		t.Fatal(err)
	}
	if o.Pass || len(o.Violations) != 1 || o.Violations[0] != "docs/decisions.md" {
		t.Fatalf("want one violation for docs/decisions.md, got %+v", o)
	}
}

func TestDeletingDecisionsWithSuperseder_IsClean(t *testing.T) {
	r := seeded(t)
	r.git("rm", "-q", "docs/decisions.md")
	r.write("docs/architecture-decisions.md", "---\nsuperseded_by: decisions.md\n---\nbody\n")
	r.commit("supersede")
	o, err := r.check()
	if err != nil {
		t.Fatal(err)
	}
	if !o.Pass || len(o.Superseded) != 1 {
		t.Fatalf("want clean with one superseded record, got %+v", o)
	}
}

func TestSupersederYAMLListForm_IsAccepted(t *testing.T) {
	r := seeded(t)
	r.git("rm", "-q", "docs/decisions.md")
	r.write("docs/adr.md", "---\nsupersedes:\n  - decisions.md\n---\nbody\n")
	r.commit("supersede")
	o, err := r.check()
	if err != nil {
		t.Fatal(err)
	}
	if !o.Pass {
		t.Fatalf("want clean with YAML-list superseder form, got %+v", o)
	}
}

func TestNonMarkdownSibling_IsNotASuperseder(t *testing.T) {
	r := seeded(t)
	r.git("rm", "-q", "docs/decisions.md")
	r.write("docs/notes.txt", "superseded_by: decisions.md\n")
	r.commit("txt only")
	o, err := r.check()
	if err != nil {
		t.Fatal(err)
	}
	if o.Pass {
		t.Fatal("a .txt file cannot supersede a protected document")
	}
}

func TestEditingAProtectedDoc_IsNotADeletion(t *testing.T) {
	r := seeded(t)
	r.write("docs/decisions.md", "# decisions\n\nedited\n")
	r.write("src/code.go", "package main\n")
	r.commit("edit")
	o, err := r.check()
	if err != nil {
		t.Fatal(err)
	}
	if !o.Pass || len(o.DeletedFiles) != 0 {
		t.Fatalf("want clean, got %+v", o)
	}
}

func TestRenamingAProtectedDoc_IsNotADeletion(t *testing.T) {
	r := seeded(t)
	r.git("mv", "docs/decisions.md", "docs/decision-record.md")
	r.commit("rename")
	o, err := r.check()
	if err != nil {
		t.Fatal(err)
	}
	if !o.Pass || len(o.DeletedFiles) != 0 {
		t.Fatalf("a rename is not a deletion, got %+v", o)
	}
}

// The gate must never pass over a protected set that resolves to nothing:
// that is how it guarded the vanished docs/wiki tree while printing "clean".
func TestProtectedDocAbsentAtBase_FailsAsGateError(t *testing.T) {
	r := newRepo(t)
	r.write("src/code.go", "package main\n")
	r.commit("seed")
	r.git("switch", "-q", "-c", "pr")
	r.commit("noop")
	_, err := r.check()
	if err == nil || !strings.Contains(err.Error(), "docs/decisions.md") {
		t.Fatalf("want an error naming the missing protected doc, got %v", err)
	}
}

func TestUnreachableBaseRef_FailsAsGateError(t *testing.T) {
	r := seeded(t)
	r.commit("noop")
	_, err := Check(Config{RepoRoot: r.root, BaseRef: "origin/main", HeadRef: "pr"})
	if err == nil || !strings.Contains(err.Error(), "BASE_REF_UNREACHABLE") {
		t.Fatalf("want BASE_REF_UNREACHABLE, got %v", err)
	}
}

func TestNonGitDirectory_FailsAsGateError(t *testing.T) {
	if _, err := Check(Config{RepoRoot: t.TempDir(), BaseRef: "main", HeadRef: "pr"}); err == nil {
		t.Fatal("want an error outside a git repository, got nil")
	}
}

func TestUnreachableHeadRef_FailsAsGateError(t *testing.T) {
	r := seeded(t)
	if _, err := Check(Config{RepoRoot: r.root, BaseRef: "main", HeadRef: "no-such-head"}); err == nil {
		t.Fatal("want an error for an unknown head ref, got nil")
	}
}
