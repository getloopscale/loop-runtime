// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// These gates read .claude/ or .squad/records/, both of which .gitignore excludes.
// On a machine without it they verified nothing, and must exit 5 — neither the
// 0 of a pass nor the 1 of a violation found in the kit.
func TestKitGatesExitFiveWithoutTheKit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Taskfile.yml"), []byte("version: '3'\ntasks:\n  a: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func([]string) int{
		"skill-paths":        runSkillPaths,
		"skill-script-paths": runSkillScriptPaths,
		"records-tree-split": runRecordsTreeSplit,
	} {
		if got := run([]string{"--root", root}); got != 5 {
			t.Errorf("%s exited %d without the kit, want 5", name, got)
		}
	}
}
