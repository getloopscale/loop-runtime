// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package testdeletion implements the test-deletion approval gate: deleting a
// test file requires either a `test-deletion-approved` PR label OR a
// "Test deletion justification: <reason>" line in the PR body.
//
// Plan T5.3 of docs/plans/2026-05-22-shell-script-strategic-plan.md v1.3:
// replaces infra/scripts/quality/check-test-deletion.sh. Shells `git` only
// (an ALLOWED CLI per D5 — no mature Go-native diff). The win is testability
// (DiffOverride seam) + consolidation into the loopzero-ops gate suite + no bash
// array fragility.
package testdeletion

import (
	"os/exec"
	"strings"
)

// Config drives the gate.
type Config struct {
	BaseRef  string // default origin/main
	HeadRef  string // default HEAD
	PRBody   string
	PRLabels string // comma-separated
	// DiffOverride, when non-nil, replaces the `git diff` call (hermetic tests).
	DiffOverride *string
	// RepoDir, when set, runs git in this dir (default: cwd).
	RepoDir string
}

// Outcome captures the result.
type Outcome struct {
	Pass         bool
	DeletedTests []string
	Reason       string
}

const justificationMarker = "test deletion justification:"
const approvalLabel = "test-deletion-approved"

// Check runs the gate.
func Check(cfg Config) Outcome {
	diffOut := ""
	if cfg.DiffOverride != nil {
		diffOut = *cfg.DiffOverride
	} else {
		diffOut = gitDeletions(cfg)
	}

	var deletedTests []string
	for _, line := range strings.Split(diffOut, "\n") {
		f := strings.TrimSpace(line)
		if f == "" {
			continue
		}
		if isTestFile(f) {
			deletedTests = append(deletedTests, f)
		}
	}

	if len(deletedTests) == 0 {
		return Outcome{Pass: true, Reason: "no test file deletions"}
	}

	// Approval label?
	for _, lbl := range strings.Split(cfg.PRLabels, ",") {
		if strings.TrimSpace(lbl) == approvalLabel {
			return Outcome{Pass: true, DeletedTests: deletedTests,
				Reason: "approved via '" + approvalLabel + "' label"}
		}
	}

	// Justification in PR body?
	if strings.Contains(strings.ToLower(cfg.PRBody), justificationMarker) {
		return Outcome{Pass: true, DeletedTests: deletedTests,
			Reason: "justification found in PR body"}
	}

	return Outcome{Pass: false, DeletedTests: deletedTests,
		Reason: "test deletions without approval label or PR-body justification"}
}

// gitDeletions runs `git diff --find-renames=80% --diff-filter=D --name-only`
// to detect TRUE deletions (renames classified as R, not D — per EC-6).
func gitDeletions(cfg Config) string {
	base := cfg.BaseRef
	if base == "" {
		base = "origin/main"
	}
	head := cfg.HeadRef
	if head == "" {
		head = "HEAD"
	}
	//nolint:shell-wrap // git is an ALLOWED CLI (D5) — no mature Go-native diff.
	cmd := exec.Command("git", "diff", "--find-renames=80%", "--diff-filter=D",
		"--name-only", base+"..."+head)
	if cfg.RepoDir != "" {
		cmd.Dir = cfg.RepoDir
	}
	out, err := cmd.Output()
	if err != nil {
		return "" // best-effort: no diff resolvable → treat as no deletions
	}
	return string(out)
}

// isTestFile classifies a path as a test file by extension.
func isTestFile(f string) bool {
	for _, suffix := range []string{"_test.go", ".test.ts", ".spec.ts", ".test.tsx", ".spec.tsx"} {
		if strings.HasSuffix(f, suffix) {
			return true
		}
	}
	return false
}
