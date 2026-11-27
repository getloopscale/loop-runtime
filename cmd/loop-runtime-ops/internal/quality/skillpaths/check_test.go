// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package skillpaths

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qtestutil"
)

func writeFixtureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// Minimal Taskfile.yml with a couple of tasks.
	must(t, os.WriteFile(filepath.Join(root, "Taskfile.yml"), []byte(`version: '3'
includes:
  k3dfile:
    taskfile: ./taskfiles/k3d.yml
    flatten: true
  quality:
    taskfile: ./taskfiles/quality.yml
tasks:
  setup:
    cmds:
      - echo setup
  clean:all:
    cmds:
      - echo clean
`), 0o644))
	// One flattened subfile.
	must(t, os.MkdirAll(filepath.Join(root, "taskfiles"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "taskfiles", "k3d.yml"), []byte(`version: '3'
tasks:
  k3d:heal:
    cmds:
      - echo heal
`), 0o644))
	// Non-flattened subfile → namespaced.
	must(t, os.WriteFile(filepath.Join(root, "taskfiles", "quality.yml"), []byte(`version: '3'
tasks:
  gates:
    cmds:
      - echo gates
`), 0o644))
	// A real script file the skill will reference.
	must(t, os.MkdirAll(filepath.Join(root, "infra", "scripts", "local"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "infra", "scripts", "local", "foo.sh"), []byte("#!/bin/bash\n"), 0o755))
	// Skills dir.
	must(t, os.MkdirAll(filepath.Join(root, ".claude", "skills", "foo"), 0o755))
	return root
}

func writeSkillMD(t *testing.T, root, skill, body string) {
	t.Helper()
	dir := filepath.Join(root, ".claude", "skills", skill)
	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheck_CanonicalPasses(t *testing.T) {
	root := writeFixtureRepo(t)
	writeSkillMD(t, root, "foo", "Uses `task setup` and script `infra/scripts/local/foo.sh` and `task quality:gates` and `task k3d:heal`.\n")
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected PASS, got err=%v failures=%v", err, outcome.Failures)
	}
}

func TestCheck_MissingScriptPathFails(t *testing.T) {
	root := writeFixtureRepo(t)
	writeSkillMD(t, root, "foo", "Uses `infra/scripts/local/does-not-exist.sh`\n")
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
}

func TestCheck_MissingTaskInvocationFails(t *testing.T) {
	root := writeFixtureRepo(t)
	writeSkillMD(t, root, "foo", "Invokes `task nonexistent-task`.\n")
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
}

func TestCheck_TaskAutomationSkillSkipped(t *testing.T) {
	root := writeFixtureRepo(t)
	// task-automation skill references non-existent task — must NOT fail
	// (skip-list carries this exemption per bash guard).
	writeSkillMD(t, root, "task-automation", "Refers to `task any-generic-task` which is not real.\n")
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected PASS (skip-list), got err=%v failures=%v", err, outcome.Failures)
	}
}

func TestCheck_WildcardsIgnored(t *testing.T) {
	root := writeFixtureRepo(t)
	writeSkillMD(t, root, "foo", "Docs pattern `infra/scripts/foo-*-bar.sh` — should not fire.\n")
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected PASS (wildcard skip), got err=%v failures=%v", err, outcome.Failures)
	}
}

func TestCheck_DogfoodGHTokenAntiPatternFails(t *testing.T) {
	root := writeFixtureRepo(t)
	writeSkillMD(t, root, "dogfood", "Uses GH_TOKEN from .env — legacy flow.\n")
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for dogfood GH_TOKEN/.env anti-pattern, got %v", err)
	}
}

func TestCheck_ToPlanTearDownAntiPatternFails(t *testing.T) {
	root := writeFixtureRepo(t)
	writeSkillMD(t, root, "to-plan", "Run `task teardown` to clean up.\n")
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for `task teardown` anti-pattern, got %v", err)
	}
}

// The skills tree is the installed kit, which .gitignore excludes, so a fresh
// clone has none. On such a machine the gate must refuse by name; where the kit
// is installed it must pass. Either way the assertion runs.
func TestCheck_RealRepoPassesOrRefusesWithoutTheKit(t *testing.T) {
	root := qtestutil.Root(t)
	outcome, checkErr := violationOf(Check(Config{RepoRoot: root}))
	if _, statErr := os.Stat(filepath.Join(root, skillsDir)); statErr != nil {
		if !errors.Is(checkErr, ErrSkillsRootMissing) {
			t.Fatalf("without the kit installed the gate must refuse by name, got %v", checkErr)
		}
		return
	}
	if checkErr != nil {
		t.Fatalf("real repo must satisfy invariant. err=%v failures=%v", checkErr, outcome.Failures)
	}
}

func TestCheck_MissingSkillsRootIsNamed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Taskfile.yml"), []byte("version: '3'\ntasks:\n  a: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := violationOf(Check(Config{RepoRoot: root})); !errors.Is(err, ErrSkillsRootMissing) {
		t.Fatalf("a missing skills root must be reported as such, got %v", err)
	}
}

// A SKILL.md that exists and cannot be read is not a skill without a SKILL.md.
func TestCheck_UnreadableSkillDocIsAnError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Taskfile.yml"), []byte("version: '3'\ntasks:\n  a: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, skillsDir, "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A directory where the file should be: present, and unreadable as a file.
	if err := os.Mkdir(filepath.Join(dir, "SKILL.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if err == nil || errors.Is(err, ErrViolations) {
		t.Fatalf("an unreadable SKILL.md was skipped, got %v", err)
	}
}
