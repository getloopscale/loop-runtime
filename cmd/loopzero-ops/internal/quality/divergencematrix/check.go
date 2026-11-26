// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package divergencematrix implements the local-prod divergence matrix
// integrity gate. Every code marker `LOCAL ONLY (LP-NNN)` must have a matching
// matrix row; every OPEN matrix row must have a live code marker.
//
// The matrix is a section of docs/operations.md — a versioned document. It
// lived under .claude/knowledge-base/ until that tree was retired, and a gate
// reading a gitignored path measured one machine's install rather than the
// repository (on any clone the gate exited 5, "matrix not found", while the
// markers it guards were still in the code).
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3 replaced
// scripts/check-divergence-matrix.sh with this package.
package divergencematrix

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// MatrixRelPath is the document that holds the matrix.
const MatrixRelPath = "docs/operations.md"

// MatrixHeading opens the matrix section inside MatrixRelPath.
const MatrixHeading = "## Local-prod divergence matrix (LP-NNN)"

// scanDirs are the code roots searched for LP-NNN markers.
var scanDirs = []string{"api", "operators", "cli/src", "pkg", "infra", "scripts"}

// excludedDirSubstrings — path fragments that disqualify a file from scanning.
var excludedDirSubstrings = []string{
	"/node_modules/", "/dist/", "/vendor/", "/.knip-cache/",
}

var (
	lpIDRe       = regexp.MustCompile(`LP-[0-9]{3}`)
	codeMarkerRe = regexp.MustCompile(`LOCAL ONLY\s+\(LP-[0-9]{3}\)`)
	// matrixRowRe matches a matrix row `| LP-NNN | <STATUS> |`.
	matrixRowRe = regexp.MustCompile(`^\|\s*(LP-[0-9]{3})\s*\|\s*([A-Z]+)\s*\|`)
)

// Config tells Check where the repo root is.
type Config struct {
	RepoRoot string
}

// Outcome captures the gate result.
type Outcome struct {
	Pass       bool
	Violations []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Check verifies matrix integrity: code markers ⟷ matrix rows.
func Check(cfg Config) (*Outcome, error) {
	codeMarkers, err := scanCodeMarkers(cfg.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("scan code markers: %w", err)
	}

	section, found, err := readMatrixSection(cfg.RepoRoot)
	if err != nil {
		return nil, err
	}
	if !found {
		return &Outcome{Violations: []string{fmt.Sprintf(
			"MATRIX_MISSING: %s has no %q section — %d LOCAL ONLY marker id(s) have nothing to be checked against",
			MatrixRelPath, MatrixHeading, len(codeMarkers))}}, nil
	}
	rows := parseMatrixRows(section)

	outcome := &Outcome{Pass: true}
	// 1. Every code marker must reference a known matrix row.
	for id, locs := range codeMarkers {
		if _, ok := rows[id]; ok {
			continue
		}
		for _, loc := range locs {
			outcome.Pass = false
			outcome.Violations = append(outcome.Violations,
				fmt.Sprintf("UNKNOWN_ID: %s referenced at %s but not in the matrix", id, loc))
		}
	}
	// 2. Every OPEN matrix row must have a live code marker.
	for id, status := range rows {
		if _, ok := codeMarkers[id]; status == "OPEN" && !ok {
			outcome.Pass = false
			outcome.Violations = append(outcome.Violations,
				fmt.Sprintf("ORPHAN_MATRIX_ENTRY: %s (OPEN status but no code marker found)", id))
		}
	}
	return outcome, nil
}

// readMatrixSection returns the body of the matrix section, from its heading
// to the next `## ` heading. A missing document or section is reported as
// found=false; any other read failure is an error.
func readMatrixSection(repoRoot string) (string, bool, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, MatrixRelPath))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", MatrixRelPath, err)
	}
	var body []string
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "## ") {
			if in {
				break
			}
			in = strings.TrimSpace(line) == MatrixHeading
			continue
		}
		if in {
			body = append(body, line)
		}
	}
	return strings.Join(body, "\n"), in || len(body) > 0, nil
}

// parseMatrixRows returns LP-NNN → status for every matrix row.
func parseMatrixRows(section string) map[string]string {
	rows := map[string]string{}
	for _, line := range strings.Split(section, "\n") {
		if m := matrixRowRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			rows[m[1]] = m[2]
		}
	}
	return rows
}

// scanCodeMarkers walks scanDirs and returns map[LP-NNN][]location. A file it
// cannot read is an error: skipping it would audit less than it reports.
func scanCodeMarkers(repoRoot string) (map[string][]string, error) {
	markers := map[string][]string{}
	for _, dir := range scanDirs {
		base := filepath.Join(repoRoot, dir)
		if _, err := os.Stat(base); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || skipped(path, d.Name()) {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			rel, rerr := filepath.Rel(repoRoot, path)
			if rerr != nil {
				return rerr
			}
			for _, marker := range codeMarkerRe.FindAllString(string(data), -1) {
				id := lpIDRe.FindString(marker)
				markers[id] = append(markers[id], rel)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return markers, nil
}

// skipped reports test files (fixtures legitimately carry example markers)
// and vendored or generated trees.
func skipped(path, name string) bool {
	if strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_test.sh") {
		return true
	}
	for _, ex := range excludedDirSubstrings {
		if strings.Contains(path, ex) {
			return true
		}
	}
	return false
}
