// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package nomigrateforce implements the CORRECTIONS.txt item B gate: fail the
// build if production code contains the literal pattern "migrate force"
// (running golang-migrate's force on a prod DB corrupts schema_migrations).
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/check-no-migrate-force.sh + its hermetic test
// (D6 AUTO-DIE). Pure-Go (filepath walk + regexp) — no external CLI.
//
// An occurrence is permitted when an allowlist marker ("# allowlist: migrate
// force" or "// allowlist: migrate force") appears on the same line OR within
// the 3 lines immediately above.
package nomigrateforce

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/linescan"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// DefaultExcludeDirs are directory names pruned anywhere in the tree. Docs
// paths (.claude, docs, review-output) legitimately reference the pattern;
// the gate targets PRODUCTION code (api/, operators/, infra/, .github/, cli/,
// pkg/, tools/).
var DefaultExcludeDirs = []string{
	".git", "node_modules", "_legacy", ".test-reports",
	".test-reports-cleanroom", "dist", "build", ".pulumi", "vendor",
	".claude", "docs", "review-output",
}

// DefaultSkipSuffixes are path suffixes always skipped: the war-journal source
// and this gate's own package (which documents/tests the pattern). Mirrors the
// bash self-exclusion of its own script + test file.
var DefaultSkipSuffixes = []string{
	"infra/tests/performance/load/CORRECTIONS.txt",
	"cmd/loopzero-ops/internal/quality/nomigrateforce/check.go",
	"cmd/loopzero-ops/internal/quality/nomigrateforce/check_test.go",
}

// Config drives the gate.
type Config struct {
	Root         string
	ExcludeDirs  []string // default DefaultExcludeDirs when nil
	SkipSuffixes []string // default DefaultSkipSuffixes when nil
}

// Outcome captures the result.
type Outcome struct {
	Pass       bool
	MatchCount int      // total matches (allowlisted + blocked)
	Blocked    []string // "path:lineno: content" for un-allowlisted matches
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

var (
	// migrate force, allowing flags between: `migrate -path X force`.
	matchRe = regexp.MustCompile(`(?i)(^|[^a-zA-Z0-9])migrate(\s+-\S+)*\s+force`)
	// allowlist marker (case-sensitive, matching the bash gate).
	allowlistRe = regexp.MustCompile(`(^|[^a-zA-Z0-9])(#|//)\s*allowlist:\s+migrate\s+force`)
)

// Check scans cfg.Root for un-allowlisted "migrate force" occurrences.
func Check(cfg Config) (*Outcome, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	excl := cfg.ExcludeDirs
	if excl == nil {
		excl = DefaultExcludeDirs
	}
	prune := make(map[string]bool, len(excl))
	for _, d := range excl {
		prune[d] = true
	}
	skip := cfg.SkipSuffixes
	if skip == nil {
		skip = DefaultSkipSuffixes
	}
	keep := func(rel string) bool {
		for _, s := range skip {
			if strings.HasSuffix(rel, s) {
				return false
			}
		}
		return true
	}

	out := &Outcome{Pass: true}
	_, err := linescan.WalkPruned(root, []string{"."}, prune, keep, func(rel string, data []byte) error {
		return scanFile(filepath.Join(root, rel), data, out)
	})
	if err != nil {
		return nil, err
	}
	if len(out.Blocked) > 0 {
		out.Pass = false
	}
	return out, nil
}

// scanFile records every "migrate force" line of data. A file it cannot split
// into lines is an error: the lines behind the failure would go unread.
func scanFile(path string, data []byte, out *Outcome) error {
	// Binary heuristic (grep -I): skip files containing a NUL byte.
	if bytes.IndexByte(data, 0) >= 0 {
		return nil
	}

	lines, err := linescan.Lines(data)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for i, line := range lines {
		if !matchRe.MatchString(line) {
			continue
		}
		out.MatchCount++
		// Same-line marker permits.
		if allowlistRe.MatchString(line) {
			continue
		}
		// Up to 3 lines BEFORE (i-3 .. i-1, 0-indexed) permit.
		permitted := false
		for j := i - 1; j >= 0 && j >= i-3; j-- {
			if allowlistRe.MatchString(lines[j]) {
				permitted = true
				break
			}
		}
		if permitted {
			continue
		}
		out.Blocked = append(out.Blocked, fmt.Sprintf("%s:%d: %s", path, i+1, line))
	}
	return nil
}
