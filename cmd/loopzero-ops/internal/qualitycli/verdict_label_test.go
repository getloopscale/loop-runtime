// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/exitcode"
)

// A gate whose chart is absent could not verify anything: it exits
// Incomplete, and its report must not also call the run a FAIL — a label that
// contradicts the exit status is the verdict mapped by hand this contract
// removes.
func TestAMissingChartIsReportedAsIncompleteNotAsAFailure(t *testing.T) {
	for _, name := range []string{
		"openbao-init-auth-mounts",
		"loopzero-external-secrets-chart-sanity",
		"loopzero-external-secrets-creation-policy",
	} {
		t.Run(name, func(t *testing.T) {
			g, ok := Lookup(name)
			if !ok {
				t.Fatalf("gate %q is not registered", name)
			}
			var stdout, stderr bytes.Buffer

			rc := g.Run([]string{"--root", t.TempDir()}, &stdout, &stderr)

			if rc != exitcode.Incomplete {
				t.Fatalf("exit = %d, want %d; stderr = %q", rc, exitcode.Incomplete, stderr.String())
			}
			if out := stdout.String() + stderr.String(); strings.Contains(out, name+": FAIL") {
				t.Fatalf("an incomplete run reported FAIL:\n%s", out)
			}
		})
	}
}

// A render pipeline that could not run (here: no tool on PATH) exits
// Incomplete, and the report must not also call it a FAIL.
func TestARenderThatCouldNotRunIsReportedAsIncompleteNotAsAFailure(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	g, ok := Lookup("render-schema-gen-sql")
	if !ok {
		t.Fatal("render-schema-gen-sql is not registered")
	}
	var stdout, stderr bytes.Buffer

	rc := g.Run([]string{"--render", "--root", t.TempDir()}, &stdout, &stderr)

	if rc != exitcode.Incomplete {
		t.Fatalf("exit = %d, want %d; stderr = %q", rc, exitcode.Incomplete, stderr.String())
	}
	if out := stdout.String() + stderr.String(); strings.Contains(out, "render-schema-gen-sql: FAIL") {
		t.Fatalf("an incomplete run reported FAIL:\n%s", out)
	}
}

// unreadableSkill builds a checkout whose kit holds one skill the gate cannot
// read, whatever the privileges of the process: its SKILL.md is a directory,
// and its tests/ is a symlink to itself.
func unreadableSkill(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	skill := filepath.Join(root, ".claude", "skills", "locked")
	if err := os.MkdirAll(filepath.Join(skill, "SKILL.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("tests", filepath.Join(skill, "tests")); err != nil {
		t.Fatal(err)
	}
	return root
}

// An input the gate cannot read is a check that could not measure: exit 5,
// never a violation. Two kit gates reported every error they did not name
// as a FAIL, exit 1.
func TestAnUnreadableSkillIsIncompleteNotAViolation(t *testing.T) {
	for _, name := range []string{"skill-script-paths", "skill-suites"} {
		t.Run(name, func(t *testing.T) {
			// skill-suites is advisory without python3; a stand-in on PATH lets the
			// run reach the unreadable skill on any machine. It is never executed:
			// discovery fails first.
			bin := t.TempDir()
			if err := os.WriteFile(filepath.Join(bin, "python3"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			g, ok := Lookup(name)
			if !ok {
				t.Fatalf("gate %q is not registered", name)
			}
			var stdout, stderr bytes.Buffer

			rc := g.Run([]string{"--root", unreadableSkill(t)}, &stdout, &stderr)

			if rc != exitcode.Incomplete {
				t.Fatalf("exit = %d, want %d; stderr = %q", rc, exitcode.Incomplete, stderr.String())
			}
			if strings.Contains(stderr.String(), name+": FAIL") {
				t.Fatalf("an unreadable input was reported as a violation:\n%s", stderr.String())
			}
		})
	}
}
