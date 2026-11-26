// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package danglingrefs

import (
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

func run(t *testing.T, root string) *Outcome {
	t.Helper()
	out, err := Check(Config{RepoRoot: root, BaseRef: "main", HeadRef: "HEAD"})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	return out
}

// TestDangling_LiveTaskRef_Fails — a deleted dr/x.sh still invoked by a task
// (full-path form) is a dangling reference → Pass=false.
func TestDangling_LiveTaskRef_Fails(t *testing.T) {
	root := qtestutil.InitRepo(t, map[string]string{
		"infra/scripts/dr/preflight.sh": "#!/usr/bin/env bash\necho hi\n",
		"taskfiles/drill.yml":           "      - bash infra/scripts/dr/preflight.sh\n",
	})
	qtestutil.CommitOn(t, root, "feat", map[string]string{
		"infra/scripts/dr/preflight.sh": "<DELETE>",
	})
	out := run(t, root)
	if out.Pass {
		t.Fatalf("expected FAIL: deleted dr/preflight.sh still referenced by a task, got Pass")
	}
	if len(out.Violations) == 0 || !strings.Contains(strings.Join(out.Violations, "\n"), "preflight.sh") {
		t.Fatalf("violation must name preflight.sh, got: %v", out.Violations)
	}
}

// TestDangling_AbbreviatedRef_Fails — EC-1: the abbreviated `dr/x.sh` form
// (no infra/scripts/ prefix) must also be caught.
func TestDangling_AbbreviatedRef_Fails(t *testing.T) {
	root := qtestutil.InitRepo(t, map[string]string{
		"infra/scripts/dr/teardown-validator.sh": "#!/usr/bin/env bash\n",
		"taskfiles/drill.yml":                    "      - bash dr/teardown-validator.sh\n",
	})
	qtestutil.CommitOn(t, root, "feat", map[string]string{
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
	root := qtestutil.InitRepo(t, map[string]string{
		"infra/scripts/dr/preflight.sh": "#!/usr/bin/env bash\n",
		"taskfiles/drill.yml":           "    # Migrated from infra/scripts/dr/preflight.sh per plan\n    cmds:\n      - /tmp/loopzero-ops drill preflight\n",
	})
	qtestutil.CommitOn(t, root, "feat", map[string]string{
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
	root := qtestutil.InitRepo(t, map[string]string{
		"infra/scripts/dr/preflight.sh":    "#!/usr/bin/env bash\n",
		"infra/tests/parity/preflight.sh":  "#!/usr/bin/env bash\n",
		".github/workflows/multi-cell.yml": "        run: bash infra/tests/parity/preflight.sh\n",
	})
	qtestutil.CommitOn(t, root, "feat", map[string]string{
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
	root := qtestutil.InitRepo(t, map[string]string{
		"infra/scripts/old-name.sh": "#!/usr/bin/env bash\necho content that is identical so git detects rename\n",
		".github/workflows/ci.yml":  "        run: bash infra/scripts/old-name.sh\n",
	})
	qtestutil.CommitOn(t, root, "feat", map[string]string{
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
	root := qtestutil.InitRepo(t, map[string]string{
		"infra/scripts/dr/watchdog.sh": "#!/usr/bin/env bash\n",
		"taskfiles/drill.yml":          "    cmds:\n      - bash infra/scripts/dr/watchdog.sh\n",
	})
	qtestutil.CommitOn(t, root, "feat", map[string]string{
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
	root := qtestutil.InitRepo(t, map[string]string{
		"infra/scripts/dr/cost-report.sh":          "#!/usr/bin/env bash\n",
		"infra/quality/shell-scripts-allowlist.md": "| infra/scripts/dr/cost-report.sh | 159 | grandfathered |\n",
	})
	qtestutil.CommitOn(t, root, "feat", map[string]string{
		"infra/scripts/dr/cost-report.sh": "<DELETE>",
	})
	out := run(t, root)
	if !out.Pass {
		t.Fatalf("allowlist reference must be excluded, got: %v", out.Violations)
	}
}

// The installed kit under .claude/ is gitignored: a SKILL there is not part of
// the repository, so it cannot make the verdict — otherwise the gate answers
// differently on every clone.
func TestDangling_GitignoredKitSkillIsNotACallerSurface(t *testing.T) {
	root := qtestutil.InitRepo(t, map[string]string{
		"infra/scripts/dr/preflight.sh":           "#!/usr/bin/env bash\necho hi\n",
		".claude/skills/cleanroom-drill/SKILL.md": "Run: `bash infra/scripts/dr/preflight.sh`\n",
	})
	qtestutil.CommitOn(t, root, "feat", map[string]string{"infra/scripts/dr/preflight.sh": "<DELETE>"})
	if out := run(t, root); !out.Pass {
		t.Fatalf("a reference inside the gitignored .claude kit must not fail the gate: %v", out.Violations)
	}
}
