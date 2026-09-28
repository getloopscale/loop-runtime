// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package devloopdocs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	taskfileRoot = filepath.Join(repoRoot, "Taskfile.yml")
	// The dev loop moved from docs/operations/dev-loop.md into the "Dev loop"
	// section of docs/operations.md when docs/ was consolidated (19b8c6eaa).
	devLoopDoc   = filepath.Join(repoRoot, "docs/operations.md")
	readmePath   = filepath.Join(repoRoot, "README.md")
	claudeMdPath = filepath.Join(repoRoot, "CLAUDE.md")
)

// The operator runbook describes the dev loop and how to troubleshoot it.
func TestOperationsDocDescribesTheDevLoop(t *testing.T) {
	data, err := os.ReadFile(devLoopDoc)
	if err != nil {
		t.Fatalf("docs/operations.md missing: %v", err)
	}
	content := string(data)
	for _, must := range []string{
		// The section heading, not the old document title: what matters is
		// that the section exists.
		"## Dev loop",
		"`task setup`",
		"`tilt up`",
		"`task doctor`",
		"Troubleshooting",
	} {
		if !strings.Contains(content, must) {
			t.Errorf("docs/operations.md should contain %q", must)
		}
	}
}

// `task setup` is one-shot; it prints a notice pointing at `tilt up` for the
// day-to-day loop.
func TestSetupTaskPointsToTiltForTheDevLoop(t *testing.T) {
	data, err := os.ReadFile(taskfileRoot)
	if err != nil {
		t.Fatalf("read Taskfile: %v", err)
	}
	content := string(data)
	// Find setup: target block (between `setup:` and next root-level task)
	startIdx := strings.Index(content, "  setup:")
	if startIdx == -1 {
		t.Fatal("setup: target not found in Taskfile.yml")
	}
	// Take 3000 chars block forward (sufficient for setup block)
	block := content[startIdx:]
	if len(block) > 3000 {
		block = block[:3000]
	}
	if !strings.Contains(block, "[NOTICE]") {
		t.Errorf("setup target should print a [NOTICE] deprecation message")
	}
	if !strings.Contains(block, "tilt up") {
		t.Errorf("setup target notice should reference 'tilt up' as the dev loop")
	}
	if !strings.Contains(block, "docs/operations.md") {
		t.Errorf("setup target notice should reference docs/operations.md")
	}
}

// The README Setup section distinguishes `task setup` from `tilt up`.
func TestReadmeSetupExplainsTheDevLoop(t *testing.T) {
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	content := string(data)
	// Find "### Setup" block
	idx := strings.Index(content, "### Setup")
	if idx == -1 {
		t.Fatal("### Setup section not found in README")
	}
	block := content[idx:]
	if len(block) > 1000 {
		block = block[:1000]
	}
	if !strings.Contains(block, "DEV LOOP") && !strings.Contains(block, "dev loop") {
		t.Errorf("README Setup section should explain dev loop = tilt up")
	}
	if !strings.Contains(block, "docs/operations.md") {
		t.Errorf("README Setup section should link to docs/operations.md")
	}
}

// CLAUDE.md's development commands distinguish the one-shot setup from the
// dev loop.
func TestClaudeMdDistinguishesOneShotSetupFromDevLoop(t *testing.T) {
	data, err := os.ReadFile(claudeMdPath)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "docs/operations.md") {
		t.Errorf("CLAUDE.md should reference docs/operations.md")
	}
	if !strings.Contains(content, "ONE-SHOT") && !strings.Contains(content, "DEV LOOP") {
		t.Errorf("CLAUDE.md should distinguish task setup (one-shot) vs tilt up (dev loop)")
	}
}
