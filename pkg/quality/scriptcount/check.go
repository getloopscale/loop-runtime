// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package scriptcount is the program-level monotonic ceiling on the number of
// canonical shell scripts (.sh) in the repository, for the Script Minimization
// Program (docs/plans/completed/2026-05-24-script-minimization-program-plan.md, T0.1).
//
// It complements:
//   - no-new-bash (blocks ADDING a .sh not in the allowlist), and
//   - unhomed-logic (counts un-homed scripts by allowlist disposition),
// by enforcing the raw canonical .sh count never regresses above a baseline.
// This catches the "half-migration leftover" failure (a Go gate is added but
// the bash it replaced is never deleted) at the program level even when
// unhomed-logic would not flag it.
//
// Design constraints:
//   - Pure Go (no shellout): filepath.WalkDir + a baseline JSON ceiling.
//   - Excludes .claude/worktrees/ (separate git checkouts — counting them
//     double-counts every script) AND trash/ (EC-1: a script git-mv'd to
//     trash/ during a migration is still tracked, so a naive find would count
//     it in the trash→delete window and give a false verdict).
package scriptcount

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
)

// excludedPathSegments — any path containing one of these directory segments
// is NOT counted. ".claude/worktrees" and "trash" are the program-critical
// exclusions (EC-1); the rest mirror the scriptsaudit scanner.
// ".claude/knowledge-base/references" holds gitignored third-party clones from
// discover cycles (cert-manager, cloudnative-pg, crossplane, etc.) — counting
// their upstream .sh makes the gate non-deterministic across machines, the same
// double-count pathology that justifies the worktrees exclusion (CHG-2026-06-24-001).
var excludedPathSegments = []string{
	"/.claude/worktrees/",
	"/.claude/knowledge-base/references/",
	"/trash/",
	"/node_modules/",
	"/vendor/",
	"/.git/",
	"/_legacy/",
}

// Baseline is the monotonic ceiling persisted at
// infra/quality/script-count-baseline.json.
type Baseline struct {
	CanonicalSH int `json:"canonical_sh"`
}

// ParseBaseline decodes the baseline JSON.
func ParseBaseline(data []byte) (Baseline, error) {
	var b Baseline
	err := json.Unmarshal(data, &b)
	return b, err
}

// CountCanonical walks root and returns the number of canonical .sh files —
// i.e. *.sh excluding the directory segments in excludedPathSegments. The
// returned paths are repo-relative with a leading "/" normalization so the
// segment match is unambiguous (a top-level trash/ matches "/trash/").
func CountCanonical(root string) (int, []string, error) {
	var matched []string
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".sh") {
			return nil
		}
		// Normalize to a "/"-anchored relative form for segment matching.
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			rel = p
		}
		anchored := "/" + filepath.ToSlash(rel)
		for _, seg := range excludedPathSegments {
			if strings.Contains(anchored, seg) {
				return nil
			}
		}
		matched = append(matched, anchored)
		return nil
	})
	if walkErr != nil {
		return 0, nil, walkErr
	}
	return len(matched), matched, nil
}

// Verdict is the gate result.
type Verdict struct {
	Count    int
	Baseline int
	OK       bool // count <= baseline
	Improved bool // count < baseline (ratchet hint)
}

// Check counts canonical .sh under root and compares to the baseline ceiling.
func Check(root string, baseline Baseline) (Verdict, error) {
	n, _, err := CountCanonical(root)
	if err != nil {
		return Verdict{}, err
	}
	return Verdict{
		Count:    n,
		Baseline: baseline.CanonicalSH,
		OK:       n <= baseline.CanonicalSH,
		Improved: n < baseline.CanonicalSH,
	}, nil
}
