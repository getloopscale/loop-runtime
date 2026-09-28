// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package taskfileinterface

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestCollectTaskNames_RootOnly — tasks defined directly in root Taskfile.
func TestCollectTaskNames_RootOnly(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Taskfile.yml"), `
version: "3"
tasks:
  setup:
    desc: foo
  doctor:
    desc: bar
`)
	names, err := CollectTaskNames(root)
	if err != nil {
		t.Fatalf("CollectTaskNames: %v", err)
	}
	if !names["setup"] || !names["doctor"] {
		t.Errorf("expected setup+doctor, got %v", names)
	}
}

// TestCollectTaskNames_FlattenedIncludes — tasks from flattened includes merge
// into root namespace (no prefix).
func TestCollectTaskNames_FlattenedIncludes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Taskfile.yml"), `
version: "3"
includes:
  dbfile:
    taskfile: ./taskfiles/db.yml
    flatten: true
tasks:
  setup:
    desc: root task
`)
	writeFile(t, filepath.Join(root, "taskfiles/db.yml"), `
version: "3"
tasks:
  db:migrate:
    desc: migrate
  db:reset:
    desc: reset
`)
	names, err := CollectTaskNames(root)
	if err != nil {
		t.Fatalf("CollectTaskNames: %v", err)
	}
	for _, want := range []string{"setup", "db:migrate", "db:reset"} {
		if !names[want] {
			t.Errorf("expected %q in collected names, got %v", want, names)
		}
	}
}

// TestCheck_AllExpectedResolve — Pass when all EXPECTED commands defined.
func TestCheck_AllExpectedResolve(t *testing.T) {
	root := t.TempDir()
	// Build a Taskfile defining every EXPECTED command.
	var sb strings.Builder
	sb.WriteString("version: \"3\"\ntasks:\n")
	for _, cmd := range ExpectedCommands {
		sb.WriteString("  \"" + cmd + "\":\n    desc: x\n")
	}
	writeFile(t, filepath.Join(root, "Taskfile.yml"), sb.String())

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("expected Pass=true, missing: %v", outcome.Missing)
	}
}

// TestCheck_MissingCommandFails — Pass=false + lists the missing command.
func TestCheck_MissingCommandFails(t *testing.T) {
	root := t.TempDir()
	// Define all but "doctor"
	var sb strings.Builder
	sb.WriteString("version: \"3\"\ntasks:\n")
	for _, cmd := range ExpectedCommands {
		if cmd == "doctor" {
			continue
		}
		sb.WriteString("  \"" + cmd + "\":\n    desc: x\n")
	}
	writeFile(t, filepath.Join(root, "Taskfile.yml"), sb.String())

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if outcome.Pass {
		t.Error("expected Pass=false (doctor missing)")
	}
	found := false
	for _, m := range outcome.Missing {
		if m == "doctor" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'doctor' in Missing, got %v", outcome.Missing)
	}
}

// TestCheck_MissingTaskfile_Errors — no Taskfile.yml → error.
func TestCheck_MissingTaskfile_Errors(t *testing.T) {
	root := t.TempDir()
	_, err := Check(Config{RepoRoot: root})
	if err == nil {
		t.Error("expected error when Taskfile.yml missing")
	}
}

// TestCollectTaskNames_SurfacesParseError — EC: an include with a duplicate
// task key (real defect Task v3 silently shadows) must be surfaced LOUD, not
// swallowed. This is how T5.3 gate caught the test:coverage duplicate.
func TestCollectTaskNames_SurfacesParseError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Taskfile.yml"), `
version: "3"
includes:
  broken:
    taskfile: ./taskfiles/broken.yml
    flatten: true
tasks:
  setup:
    desc: x
`)
	// broken.yml has a duplicate task key
	writeFile(t, filepath.Join(root, "taskfiles/broken.yml"), `
version: "3"
tasks:
  dup:
    desc: first
  dup:
    desc: second
`)
	_, err := CollectTaskNames(root)
	if err == nil {
		t.Fatal("expected parse error surfaced for duplicate key, got nil")
	}
	if !strings.Contains(err.Error(), "broken Taskfile") {
		t.Errorf("error should flag broken Taskfile, got: %v", err)
	}
}

// TestExpectedCommands_NonEmpty — sanity: the contract list is populated.
func TestExpectedCommands_NonEmpty(t *testing.T) {
	if len(ExpectedCommands) == 0 {
		t.Error("ExpectedCommands must not be empty")
	}
	// Must include the core onboarding commands
	core := map[string]bool{"setup": false, "doctor": false, "test": false}
	for _, cmd := range ExpectedCommands {
		if _, ok := core[cmd]; ok {
			core[cmd] = true
		}
	}
	for cmd, present := range core {
		if !present {
			t.Errorf("ExpectedCommands missing core command %q", cmd)
		}
	}
}

// An include without `flatten: true` puts its tasks under the include's name,
// the way Task v3 does: a task `setup` in a namespaced include is `drill:setup`,
// and must not satisfy a public command spelled `setup`.
func TestCollectTaskNames_NamespacesNonFlattenedIncludes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Taskfile.yml"), `
version: "3"
includes:
  drill:
    taskfile: ./taskfiles/drill.yml
tasks:
  doctor:
    desc: root task
`)
	writeFile(t, filepath.Join(root, "taskfiles/drill.yml"), `
version: "3"
tasks:
  setup:
    desc: drill setup
`)

	names, err := CollectTaskNames(root)

	if err != nil {
		t.Fatalf("CollectTaskNames: %v", err)
	}
	if names["setup"] {
		t.Error("a task of a non-flattened include must not appear under its bare name")
	}
	if !names["drill:setup"] {
		t.Errorf("expected drill:setup, got %v", names)
	}
}

// A missing include that is not declared `optional: true` breaks `task` itself,
// so the inventory must not quietly drop it.
func TestCollectTaskNames_MissingRequiredIncludeIsAnError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Taskfile.yml"), `
version: "3"
includes:
  drill:
    taskfile: ./taskfiles/drill.yml
tasks:
  doctor: {}
`)

	if _, err := CollectTaskNames(root); err == nil {
		t.Fatal("a missing non-optional include must be an error")
	}
}

func TestCollectTaskNames_MissingOptionalIncludeIsSkipped(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Taskfile.yml"), `
version: "3"
includes:
  extra:
    taskfile: ./taskfiles/extra.yml
    optional: true
tasks:
  doctor: {}
`)

	names, err := CollectTaskNames(root)

	if err != nil || !names["doctor"] {
		t.Fatalf("an optional missing include must be skipped; names=%v err=%v", names, err)
	}
}
