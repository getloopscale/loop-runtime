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

// initTestRepo builds a hermetic git repo in t.TempDir with the given files
// committed on `main` branch. Returns the repo root path.
func initTestRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	mustRun(t, root, "git", "init", "-q", "--initial-branch=main")
	mustRun(t, root, "git", "config", "user.email", "test@loopzero.local")
	mustRun(t, root, "git", "config", "user.name", "test")
	mustRun(t, root, "git", "config", "commit.gpgsign", "false")
	for relPath, content := range files {
		full := filepath.Join(root, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	mustRun(t, root, "git", "add", "-A")
	mustRun(t, root, "git", "commit", "-q", "-m", "initial")
	return root
}

// commitOn creates a feature branch and commits the given file mutations.
// `mutations` map values: string content for add/modify, "" string for delete.
func commitOn(t *testing.T, root, branch string, mutations map[string]string) {
	t.Helper()
	mustRun(t, root, "git", "checkout", "-q", "-b", branch)
	for relPath, content := range mutations {
		full := filepath.Join(root, relPath)
		if content == "<DELETE>" {
			if err := os.Remove(full); err != nil {
				t.Fatalf("remove %s: %v", full, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	mustRun(t, root, "git", "add", "-A")
	mustRun(t, root, "git", "commit", "-q", "-m", "feature change")
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// TestParseAllowlist_ParsesMarkdownTable — allowlist MD with table rows is
// parsed into a set of script paths.
func TestParseAllowlist_ParsesMarkdownTable(t *testing.T) {
	content := `# Shell Scripts Allowlist

## infra/scripts/

| Script | LOC | Justification |
|---|---:|---|
| infra/scripts/break-glass.sh | 145 | Operator-time emergency |
| infra/scripts/drill-jwt-gate.sh | 50 | Drill JWT validation |

## Other

| Script | LOC | Notes |
|---|---:|---|
| .claude/hooks/internal-terms.py | 80 | Hook (excluded from gate scope) |
`
	allowed, err := parseAllowlist(content)
	if err != nil {
		t.Fatalf("parseAllowlist: %v", err)
	}
	if !allowed["infra/scripts/break-glass.sh"] {
		t.Error("expected break-glass.sh in allowlist")
	}
	if !allowed["infra/scripts/drill-jwt-gate.sh"] {
		t.Error("expected drill-jwt-gate.sh in allowlist")
	}
	if allowed["nonexistent.sh"] {
		t.Error("nonexistent.sh should not be in allowlist")
	}
}

// TestCheck_NewBashWithoutAllowlist_Blocks — diff with new .sh outside allowlist → Pass=false
func TestCheck_NewBashWithoutAllowlist_Blocks(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"infra/scripts/existing.sh": "#!/bin/bash\necho existing\n",
		".allowlist.md":             "| infra/scripts/existing.sh | 1 | grandfathered |\n",
	})
	commitOn(t, root, "feature", map[string]string{
		"infra/scripts/new-script.sh": "#!/bin/bash\necho new\n",
	})

	outcome, err := Check(Config{
		RepoRoot:      root,
		AllowlistPath: ".allowlist.md",
		BaseRef:       "main",
		HeadRef:       "feature",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if outcome.Pass {
		t.Error("expected Pass=false (new script outside allowlist)")
	}
	if len(outcome.Violations) != 1 {
		t.Errorf("expected 1 violation, got %d: %v", len(outcome.Violations), outcome.Violations)
	}
	if !strings.Contains(outcome.Violations[0], "new-script.sh") {
		t.Errorf("violation should mention new-script.sh, got %s", outcome.Violations[0])
	}
}

// TestCheck_NewBashWithAllowlist_Passes — diff with new .sh that IS in allowlist → Pass=true
func TestCheck_NewBashWithAllowlist_Passes(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"infra/scripts/existing.sh": "#!/bin/bash\necho existing\n",
		".allowlist.md":             "| infra/scripts/existing.sh | 1 | grandfathered |\n| infra/scripts/new-allowed.sh | 1 | freshly added with ADR |\n",
	})
	commitOn(t, root, "feature", map[string]string{
		"infra/scripts/new-allowed.sh": "#!/bin/bash\necho allowed\n",
	})

	outcome, err := Check(Config{
		RepoRoot:      root,
		AllowlistPath: ".allowlist.md",
		BaseRef:       "main",
		HeadRef:       "feature",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("expected Pass=true, got violations: %v", outcome.Violations)
	}
}

// TestCheck_ModifiedExistingBash_NotBlocked — M (modified) status is NOT blocked
// (only A status triggers the gate).
func TestCheck_ModifiedExistingBash_NotBlocked(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"infra/scripts/existing.sh": "#!/bin/bash\necho v1\n",
		".allowlist.md":             "| infra/scripts/existing.sh | 1 | grandfathered |\n",
	})
	commitOn(t, root, "feature", map[string]string{
		"infra/scripts/existing.sh": "#!/bin/bash\necho v2\n", // modify, not add
	})

	outcome, err := Check(Config{
		RepoRoot:      root,
		AllowlistPath: ".allowlist.md",
		BaseRef:       "main",
		HeadRef:       "feature",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("expected Pass=true (only M, no A); got violations: %v", outcome.Violations)
	}
}

// TestCheck_ShallowClone_FailsWithClearError — EC-04: when base-ref is
// unreachable, fail loud with BASE_REF_UNREACHABLE (do NOT produce 100 false positives).
func TestCheck_ShallowClone_FailsWithClearError(t *testing.T) {
	root := t.TempDir()
	// Init empty repo with only HEAD commit (no main branch)
	mustRun(t, root, "git", "init", "-q", "--initial-branch=feature")
	mustRun(t, root, "git", "config", "user.email", "test@loopzero.local")
	mustRun(t, root, "git", "config", "user.name", "test")
	mustRun(t, root, "git", "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(root, ".allowlist.md"), []byte(""), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustRun(t, root, "git", "add", "-A")
	mustRun(t, root, "git", "commit", "-q", "-m", "feature-only")

	_, err := Check(Config{
		RepoRoot:      root,
		AllowlistPath: ".allowlist.md",
		BaseRef:       "origin/main", // intentionally unreachable
		HeadRef:       "HEAD",
	})
	if err == nil {
		t.Fatal("expected BASE_REF_UNREACHABLE error, got nil")
	}
	if !strings.Contains(err.Error(), "BASE_REF_UNREACHABLE") {
		t.Errorf("expected BASE_REF_UNREACHABLE in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "fetch-depth") {
		t.Errorf("error should mention fetch-depth hint, got: %v", err)
	}
}

// TestCheck_RenamedBashOutsideAllowlist_Blocked — EC-06: file rename (git status R)
// is treated as new file when new name is not in allowlist.
func TestCheck_RenamedBashOutsideAllowlist_Blocked(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"infra/scripts/old-name.sh": "#!/bin/bash\necho oldname\n",
		".allowlist.md":             "| infra/scripts/old-name.sh | 1 | grandfathered |\n",
	})
	// Rename via git mv on feature branch
	mustRun(t, root, "git", "checkout", "-q", "-b", "feature")
	mustRun(t, root, "git", "mv", "infra/scripts/old-name.sh", "infra/scripts/renamed.sh")
	mustRun(t, root, "git", "commit", "-q", "-m", "rename")

	outcome, err := Check(Config{
		RepoRoot:      root,
		AllowlistPath: ".allowlist.md",
		BaseRef:       "main",
		HeadRef:       "feature",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if outcome.Pass {
		t.Errorf("expected Pass=false (rename to unallowlisted name); violations: %v", outcome.Violations)
	}
	foundRename := false
	for _, v := range outcome.Violations {
		if strings.Contains(v, "renamed.sh") {
			foundRename = true
		}
	}
	if !foundRename {
		t.Errorf("violation should mention renamed.sh, got: %v", outcome.Violations)
	}
}

// TestCheck_DeletedBash_NotBlocked — D (deleted) status is NOT blocked.
func TestCheck_DeletedBash_NotBlocked(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"infra/scripts/dying.sh": "#!/bin/bash\necho rip\n",
		".allowlist.md":          "| infra/scripts/dying.sh | 1 | grandfathered |\n",
	})
	commitOn(t, root, "feature", map[string]string{
		"infra/scripts/dying.sh": "<DELETE>",
	})

	outcome, err := Check(Config{
		RepoRoot:      root,
		AllowlistPath: ".allowlist.md",
		BaseRef:       "main",
		HeadRef:       "feature",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("expected Pass=true (delete is fine); violations: %v", outcome.Violations)
	}
}

// TestParseAllowlist_HandlesEmptyFile — empty allowlist parses to empty set
func TestParseAllowlist_HandlesEmptyFile(t *testing.T) {
	allowed, err := parseAllowlist("")
	if err != nil {
		t.Fatalf("parseAllowlist: %v", err)
	}
	if len(allowed) != 0 {
		t.Errorf("expected empty allowlist, got %d entries", len(allowed))
	}
}

// TestParseAllowlist_IgnoresNonSh — entries not ending in .sh are ignored
func TestParseAllowlist_IgnoresNonSh(t *testing.T) {
	content := `
| Script | LOC | Notes |
|---|---:|---|
| infra/scripts/foo.sh | 1 | shell |
| .claude/hooks/X.py | 1 | python hook |
| Makefile | 1 | make |
`
	allowed, err := parseAllowlist(content)
	if err != nil {
		t.Fatalf("parseAllowlist: %v", err)
	}
	if len(allowed) != 1 || !allowed["infra/scripts/foo.sh"] {
		t.Errorf("expected only foo.sh, got %v", allowed)
	}
}

// === Additional coverage tests ===

// An absent allowlist exempts NOTHING, and the gate still decides.
//
// This test asserted the opposite until 2026-09-16 — that a missing allowlist is an
// infrastructure failure — and that was the right reading while the file was tracked.
// `3d4075ca3` (2026-09-08) removed `.claude/` from tracking and took the allowlist
// with it, so the fatal read made this gate unrunnable in every clone: measured that
// day, it exited 5 and blocked the repository's own pre-push hook.
//
// The substance the old assertion protected — an infrastructure failure must not be
// swallowed — did not move. It is asserted by TestCheck_UnreadableAllowlistIsStillAnError,
// against a path that exists and cannot be read. What changed is only the answer for
// ABSENCE, and the new answer is the stricter one: with no allowlist, nothing is exempt.
func TestCheck_AbsentAllowlistExemptsNothing(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"infra/scripts/x.sh": "#!/bin/bash\n",
	})
	out, err := Check(Config{
		RepoRoot:      root,
		AllowlistPath: ".does-not-exist.md",
		BaseRef:       "main",
		HeadRef:       "main",
	})
	if err != nil {
		t.Fatalf("an absent allowlist is not an infrastructure failure, got: %v", err)
	}
	if out == nil {
		t.Fatal("expected an outcome, got nil")
	}
}

// TestCheck_NoDiff_PassesTrivially — base == head produces empty diff, gate passes.
func TestCheck_NoDiff_PassesTrivially(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"infra/scripts/x.sh": "#!/bin/bash\n",
		".allowlist.md":      "| infra/scripts/x.sh | 1 | g |\n",
	})
	outcome, err := Check(Config{
		RepoRoot:      root,
		AllowlistPath: ".allowlist.md",
		BaseRef:       "main",
		HeadRef:       "main",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("expected Pass=true when no diff; got %v", outcome.Violations)
	}
}

// TestParseAllowlist_HeaderSeparatorIgnored — `|---|---|` rows are not matched
func TestParseAllowlist_HeaderSeparatorIgnored(t *testing.T) {
	content := `
| Script | LOC |
|---|---:|
| foo.sh | 1 |
`
	allowed, err := parseAllowlist(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(allowed) != 1 || !allowed["foo.sh"] {
		t.Errorf("expected only foo.sh, got %v", allowed)
	}
}
