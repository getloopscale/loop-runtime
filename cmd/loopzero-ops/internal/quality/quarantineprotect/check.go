// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package quarantineprotect implements the T0.2 governance gate from
// plan canonical-tests-rewrite-v2 v1.0 per ADR-2026-06-17-T3-canonical-tests-rewrite D5
// (translated from the Portuguese original):
//
//	"Quarantine = read-only consultable files (moved to archive/canonical-tests-v1/...
//	+ a CI gate rejects PRs editing quarantined files). The old chain stays executable
//	during the window. Exit criterion: ... definitive DELETE of the old one."
//
// The gate scans `git diff --diff-filter=AMD base...head -- archive/canonical-tests-v1/`
// and fails if any path under archive/canonical-tests-v1/ is ADDED or MODIFIED.
// DELETE operations are allowed ONLY when the PR title contains the marker
// `[ARCHIVE-DELETE]` (anti-accidental-delete safeguard).
//
// EC-04 (from nonewbash precedent): gate fails LOUD with BASE_REF_UNREACHABLE
// when base-ref is unreachable (shallow clone).
package quarantineprotect

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/revrange"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// archiveDeleteMarker is the PR title substring that unlocks DELETE operations
// inside the archive directory. Anti-accidental-delete safeguard per ADR D5
// "exit criterion: ... + sponsor verbatim sign-off → DELETE".
const archiveDeleteMarker = "[ARCHIVE-DELETE]"

// Config tells Check where to operate.
type Config struct {
	RepoRoot        string // absolute path to repo root
	ArchivePathRoot string // path prefix to enforce (e.g., "archive/canonical-tests-v1")
	BaseRef         string // base git ref (e.g., "origin/main")
	HeadRef         string // head git ref (e.g., "HEAD")
	PRTitle         string // PR title (substring-matched for archiveDeleteMarker)

	// Mock fields (hermetic test mode). When mockMode=true, Check uses these
	// instead of invoking git.
	DiffAddedOrMod []string
	DiffDeleted    []string
	mockMode       bool
}

// Outcome captures the result of a Check invocation.
type Outcome struct {
	Pass       bool     // true if no violations
	Violations []string // human-readable per-violation messages
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Check is the entry point invoked by `loopzero-ops quality quarantine-protect`.
// Returns outcome + error. Error is reserved for INFRASTRUCTURE failures
// (git missing, base-ref unreachable); policy violations come back via
// Outcome.Violations + Pass=false.
func Check(cfg Config) (*Outcome, error) {
	var addedOrMod, deleted []string
	var err error

	if cfg.mockMode {
		addedOrMod = cfg.DiffAddedOrMod
		deleted = cfg.DiffDeleted
	} else {
		// Pre-flight: verify base-ref reachable (EC-04 from nonewbash).
		if err := revrange.VerifyReachable(cfg.RepoRoot, cfg.BaseRef); err != nil {
			return nil, err
		}
		addedOrMod, err = gitDiffAddedOrModified(cfg.RepoRoot, cfg.BaseRef, cfg.HeadRef, cfg.ArchivePathRoot)
		if err != nil {
			return nil, fmt.Errorf("git diff added/modified: %w", err)
		}
		deleted, err = gitDiffDeleted(cfg.RepoRoot, cfg.BaseRef, cfg.HeadRef, cfg.ArchivePathRoot)
		if err != nil {
			return nil, fmt.Errorf("git diff deleted: %w", err)
		}
	}

	outcome := &Outcome{Pass: true}
	prefix := cfg.ArchivePathRoot + "/"

	// Rule 1: ADD or MODIFY in archive/canonical-tests-v1/** is REJECTED.
	// Marker `[ARCHIVE-DELETE]` does NOT allow ADD/MODIFY — only DELETE.
	for _, path := range addedOrMod {
		if isUnder(path, prefix) {
			outcome.Pass = false
			outcome.Violations = append(outcome.Violations, fmt.Sprintf(
				"%s/ is READ-ONLY per ADR-2026-06-17-T3 Amendment 1; modifying %q is forbidden until DELETE phase. "+
					"If you need to update the canonical chain, work on the v2 paths at infra/tests/livetest/ instead. "+
					"Plan: knowledge-base/plans/canonical-tests-rewrite-v2-plan.md.",
				cfg.ArchivePathRoot, path))
		}
	}

	// Rule 2: DELETE in archive/canonical-tests-v1/** is allowed ONLY when PR title contains marker.
	hasMarker := strings.Contains(cfg.PRTitle, archiveDeleteMarker)
	for _, path := range deleted {
		if isUnder(path, prefix) && !hasMarker {
			outcome.Pass = false
			outcome.Violations = append(outcome.Violations, fmt.Sprintf(
				"deletion of %q under quarantine path %s is forbidden without the %s marker in the PR title. "+
					"Anti-accidental-delete safeguard per ADR D5 exit criterion: requires sponsor verbatim sign-off "+
					"after 3 consecutive chain-of-6-stages GREEN. Add %s to your PR title only when CP3 is recorded.",
				path, cfg.ArchivePathRoot, archiveDeleteMarker, archiveDeleteMarker))
		}
	}

	return outcome, nil
}

// isUnder reports whether path is under prefix. Uses "/"-delimited string match
// because git always emits forward slashes regardless of OS.
func isUnder(path, prefix string) bool {
	return strings.HasPrefix(path, prefix)
}

// gitDiffAddedOrModified returns paths under archivePathRoot with status A (added) or M (modified)
// between baseRef and headRef. Uses `git diff --diff-filter=AM --name-only base...head -- path`.
func gitDiffAddedOrModified(repoRoot, baseRef, headRef, archivePathRoot string) ([]string, error) {
	return runGitDiff(repoRoot, "AM", baseRef, headRef, archivePathRoot)
}

// gitDiffDeleted returns paths under archivePathRoot with status D (deleted).
func gitDiffDeleted(repoRoot, baseRef, headRef, archivePathRoot string) ([]string, error) {
	return runGitDiff(repoRoot, "D", baseRef, headRef, archivePathRoot)
}

func runGitDiff(repoRoot, filter, baseRef, headRef, pathPrefix string) ([]string, error) {
	args := []string{
		"diff",
		"--name-only",
		"--diff-filter=" + filter,
		baseRef + "..." + headRef,
		"--",
		pathPrefix,
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %v: %w", args, err)
	}
	var paths []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		paths = append(paths, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan diff output: %w", err)
	}
	return paths, nil
}
