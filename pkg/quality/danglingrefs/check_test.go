// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package danglingrefs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTestRepo builds a hermetic git repo in t.TempDir with the given files
// committed on `main`. Returns repo root.
func initTestRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	mustRun(t, root, "git", "init", "-q", "--initial-branch=main")
	mustRun(t, root, "git", "config", "user.email", "test@loopzero.local")
	mustRun(t, root, "git", "config", "user.name", "test")
	mustRun(t, root, "git", "config", "commit.gpgsign", "false")
	writeAll(t, root, files)
	mustRun(t, root, "git", "add", "-A")
	mustRun(t, root, "git", "commit", "-q", "-m", "initial")
	return root
}

// commitOn creates a feature branch and applies mutations. Value "<DELETE>"
// removes the file; "<RENAME>:newpath" git-mv's old→new; else add/modify.
func commitOn(t *testing.T, root, branch string, mutations map[string]string) {
	t.Helper()
	mustRun(t, root, "git", "checkout", "-q", "-b", branch)
	for relPath, content := range mutations {
		switch {
		case content == "<DELETE>":
			mustRun(t, root, "git", "rm", "-q", relPath)
		case strings.HasPrefix(content, "<RENAME>:"):
			dst := strings.TrimPrefix(content, "<RENAME>:")
			full := filepath.Join(root, dst)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			mustRun(t, root, "git", "mv", relPath, dst)
		default:
			full := filepath.Join(root, relPath)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
				t.Fatalf("write %s: %v", relPath, err)
			}
		}
	}
	mustRun(t, root, "git", "add", "-A")
	mustRun(t, root, "git", "commit", "-q", "-m", "feature change")
}

func writeAll(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for relPath, content := range files {
		full := filepath.Join(root, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", relPath, err)
		}
	}
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

func run(t *testing.T, root string) *Outcome {
	t.Helper()
	out, err := Check(Config{RepoRoot: root, BaseRef: "main", HeadRef: "HEAD"})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	return out
}

// TestDangling_LiveSkillRef_Fails — a deleted dr/x.sh still invoked by a SKILL
// (full-path form) is a dangling reference → Pass=false.
func TestDangling_LiveSkillRef_Fails(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"infra/scripts/dr/preflight.sh":         "#!/usr/bin/env bash\necho hi\n",
		".claude/skills/cleanroom-drill/SKILL.md": "Run: `bash infra/scripts/dr/preflight.sh`\n",
	})
	commitOn(t, root, "feat", map[string]string{
		"infra/scripts/dr/preflight.sh": "<DELETE>",
	})
	out := run(t, root)
	if out.Pass {
		t.Fatalf("expected FAIL: deleted dr/preflight.sh still referenced by SKILL, got Pass")
	}
	if len(out.Violations) == 0 || !strings.Contains(strings.Join(out.Violations, "\n"), "preflight.sh") {
		t.Fatalf("violation must name preflight.sh, got: %v", out.Violations)
	}
}

// TestDangling_AbbreviatedSkillRef_Fails — EC-1: the abbreviated `dr/x.sh` form
// (no infra/scripts/ prefix) the SKILLs actually use must also be caught.
func TestDangling_AbbreviatedSkillRef_Fails(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"infra/scripts/dr/teardown-validator.sh": "#!/usr/bin/env bash\n",
		".claude/skills/drill-test/SKILL.md":      "- Pular `dr/teardown-validator.sh` (audit cleanup)\n",
	})
	commitOn(t, root, "feat", map[string]string{
		"infra/scripts/dr/teardown-validator.sh": "<DELETE>",
	})
	out := run(t, root)
	if out.Pass {
		t.Fatalf("EC-1: abbreviated dr/teardown-validator.sh ref must be caught, got Pass")
	}
}

// TestDangling_CommentOnly_Passes — a deleted script referenced ONLY in a
// provenance comment ("Migrated from ...") is not a live reference → Pass.
func TestDangling_CommentOnly_Passes(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"infra/scripts/dr/preflight.sh": "#!/usr/bin/env bash\n",
		"taskfiles/drill.yml":           "    # Migrated from infra/scripts/dr/preflight.sh per plan\n    cmds:\n      - /tmp/loopzero-ops drill preflight\n",
	})
	commitOn(t, root, "feat", map[string]string{
		"infra/scripts/dr/preflight.sh": "<DELETE>",
	})
	out := run(t, root)
	if !out.Pass {
		t.Fatalf("comment-only provenance ref must PASS, got violations: %v", out.Violations)
	}
}

// TestDangling_BasenameCollision_Passes — dr/preflight.sh deleted, but a
// DIFFERENT preflight.sh (tests/parity/) survives and is referenced. The bare
// basename must NOT trigger a false positive.
func TestDangling_BasenameCollision_Passes(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"infra/scripts/dr/preflight.sh":      "#!/usr/bin/env bash\n",
		"infra/tests/parity/preflight.sh":    "#!/usr/bin/env bash\n",
		".github/workflows/multi-cell.yml":   "        run: bash infra/tests/parity/preflight.sh\n",
	})
	commitOn(t, root, "feat", map[string]string{
		"infra/scripts/dr/preflight.sh": "<DELETE>",
	})
	out := run(t, root)
	if !out.Pass {
		t.Fatalf("basename collision must PASS (parity/preflight.sh survives), got: %v", out.Violations)
	}
}

// TestDangling_Rename_TreatedAsDelete — renaming a script (R) while the OLD
// path is still referenced is a dangling reference.
func TestDangling_Rename_TreatedAsDelete(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"infra/scripts/old-name.sh":   "#!/usr/bin/env bash\necho content that is identical so git detects rename\n",
		".github/workflows/ci.yml":     "        run: bash infra/scripts/old-name.sh\n",
	})
	commitOn(t, root, "feat", map[string]string{
		"infra/scripts/old-name.sh": "<RENAME>:infra/scripts/new-name.sh",
	})
	out := run(t, root)
	if out.Pass {
		t.Fatalf("renamed-away old-name.sh still referenced must FAIL, got Pass")
	}
}

// TestDangling_DeletedAndAllRefsRemoved_Passes — the happy path: a script is
// deleted AND all its references are gone in the same change → Pass.
func TestDangling_DeletedAndAllRefsRemoved_Passes(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"infra/scripts/dr/watchdog.sh": "#!/usr/bin/env bash\n",
		"taskfiles/drill.yml":          "    cmds:\n      - bash infra/scripts/dr/watchdog.sh\n",
	})
	commitOn(t, root, "feat", map[string]string{
		"infra/scripts/dr/watchdog.sh": "<DELETE>",
		"taskfiles/drill.yml":          "    cmds:\n      - /tmp/loopzero-ops drill watchdog\n",
	})
	out := run(t, root)
	if !out.Pass {
		t.Fatalf("deletion with all refs rewired must PASS, got: %v", out.Violations)
	}
}

// TestDangling_AllowlistRefIgnored_Passes — the allowlist MD legitimately names
// every script; a deleted script lingering in the allowlist is not "live".
func TestDangling_AllowlistRefIgnored_Passes(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"infra/scripts/dr/cost-report.sh":            "#!/usr/bin/env bash\n",
		".claude/rules/shell-scripts-allowlist.md":   "| infra/scripts/dr/cost-report.sh | 159 | grandfathered |\n",
	})
	commitOn(t, root, "feat", map[string]string{
		"infra/scripts/dr/cost-report.sh": "<DELETE>",
	})
	out := run(t, root)
	if !out.Pass {
		t.Fatalf("allowlist reference must be excluded, got: %v", out.Violations)
	}
}
