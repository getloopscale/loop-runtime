// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package testdeletion implements the test-deletion approval gate: deleting a
// test file requires either a `test-deletion-approved` PR label OR a
// "Test deletion justification: <reason>" line in the PR body.
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/quality/check-test-deletion.sh. Shells `git` only
// (an ALLOWED CLI per D5 — no mature Go-native diff). The win is testability
// (DiffOverride seam) + consolidation into the loopzero-ops gate suite + no bash
// array fragility.
package testdeletion

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/revrange"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// Config drives the gate.
type Config struct {
	BaseRef  string // default origin/main
	HeadRef  string // default HEAD
	PRBody   string
	PRLabels string // comma-separated
	// CommitLog, when non-empty, replaces the `git log` call (hermetic tests).
	// Empty + no override → the log of BaseRef...HeadRef is read from git.
	CommitLog string
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

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

const justificationMarker = "test deletion justification:"
const approvalLabel = "test-deletion-approved"

// Check runs the gate. It returns an error when the range cannot be read — an
// unreachable base (a shallow clone) or a git failure — because "no deletions"
// is a verdict about a comparison, and there was none.
func Check(cfg Config) (Outcome, error) {
	var diffOut string
	if cfg.DiffOverride != nil {
		diffOut = *cfg.DiffOverride
	} else {
		if err := revrange.VerifyReachable(cfg.RepoDir, baseRef(cfg)); err != nil {
			return Outcome{}, err
		}
		var err error
		if diffOut, err = gitDeletions(cfg); err != nil {
			return Outcome{}, err
		}
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
		return Outcome{Pass: true, Reason: "no test file deletions"}, nil
	}

	// Approval label?
	for _, lbl := range strings.Split(cfg.PRLabels, ",") {
		if strings.TrimSpace(lbl) == approvalLabel {
			return Outcome{Pass: true, DeletedTests: deletedTests,
				Reason: "approved via '" + approvalLabel + "' label"}, nil
		}
	}

	// Justification in PR body?
	if strings.Contains(strings.ToLower(cfg.PRBody), justificationMarker) {
		return Outcome{Pass: true, DeletedTests: deletedTests,
			Reason: "justification found in PR body"}, nil
	}

	// The commit body is the only channel that lives in the repository. PR body
	// and label are both outside it: the pre-push hook has neither, so a
	// legitimate deletion could not be pushed at all, and both can be edited
	// after the fact while the commit that did the deleting cannot.
	// DiffOverride marks a hermetic run: the caller supplied the diff, so the
	// commit log must come from the caller too. Falling back to `git log` here
	// let the REAL repository history decide a unit test's verdict — and the
	// moment a commit in this repo carried the marker, every "deletion without
	// justification must fail" test started passing. Caught 2026-08-06 by the
	// delivery-gate hook on the very commit that introduced it.
	log := cfg.CommitLog
	if log == "" && cfg.DiffOverride == nil {
		var err error
		if log, err = gitCommitLog(cfg); err != nil {
			return Outcome{}, err
		}
	}
	if strings.Contains(strings.ToLower(log), justificationMarker) {
		return Outcome{Pass: true, DeletedTests: deletedTests,
			Reason: "justification found in a commit message"}, nil
	}

	return Outcome{Pass: false, DeletedTests: deletedTests,
		Reason: "test deletions without approval label or PR-body justification"}, nil
}

func baseRef(cfg Config) string {
	if cfg.BaseRef == "" {
		return "origin/main"
	}
	return cfg.BaseRef
}

func headRef(cfg Config) string {
	if cfg.HeadRef == "" {
		return "HEAD"
	}
	return cfg.HeadRef
}

// gitCommitLog returns the commit messages in BaseRef...HeadRef, where the
// justification for a deletion naturally lives — next to the deletion.
func gitCommitLog(cfg Config) (string, error) {
	return runGit(cfg, "log", "--format=%B", baseRef(cfg)+"..."+headRef(cfg))
}

// gitDeletions runs `git diff --find-renames=80% --diff-filter=D --name-only`
// to detect TRUE deletions (renames are classified R, not D).
func gitDeletions(cfg Config) (string, error) {
	return runGit(cfg, "diff", "--find-renames=80%", "--diff-filter=D", "--name-only", baseRef(cfg)+"..."+headRef(cfg))
}

// runGit runs git in cfg.RepoDir. git is an allowed CLI (D5): there is no
// mature Go-native diff or log.
func runGit(cfg Config, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = cfg.RepoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
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
