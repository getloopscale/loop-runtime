// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package divergencematrix implements the local-prod divergence matrix
// integrity gate. Every code marker `LOCAL ONLY (LP-NNN)` must have a matching
// matrix entry; every OPEN matrix entry must have a live code marker.
//
// Plan T5.3 of docs/plans/2026-05-22-shell-script-strategic-plan.md v1.3:
// replaces scripts/check-divergence-matrix.sh. Pure-logic Go (filepath.WalkDir
// + regexp) — no shell-out. Per D5: a genuine pure-logic migration.
package divergencematrix

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MatrixRelPath is the canonical matrix location.
const MatrixRelPath = ".claude/knowledge-base/concepts/operations/local-prod-divergence-matrix.md"

// LegacyMatrixRelPath is the pre-2026-05-11 location (fallback).
const LegacyMatrixRelPath = "docs/runbooks/local-prod-divergence-matrix.md"

// scanDirs are the code roots searched for LP-NNN markers.
var scanDirs = []string{"api", "operators", "cli/src", "pkg", "infra", "scripts"}

// excludedDirSubstrings — path fragments that disqualify a file from scanning.
var excludedDirSubstrings = []string{
	"/node_modules/", "/dist/", "/vendor/", "/.knip-cache/",
}

var (
	lpIDRe       = regexp.MustCompile(`LP-[0-9]{3}`)
	codeMarkerRe = regexp.MustCompile(`LOCAL ONLY\s+\(LP-[0-9]{3}\)`)
	// matrixRowRe matches a matrix table row beginning with `| LP-NNN`.
	matrixRowRe = regexp.MustCompile(`^\|\s*LP-[0-9]{3}`)
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

// Check verifies matrix integrity: code markers ⟷ matrix entries.
func Check(cfg Config) (*Outcome, error) {
	matrixPath := filepath.Join(cfg.RepoRoot, MatrixRelPath)
	if _, err := os.Stat(matrixPath); os.IsNotExist(err) {
		legacy := filepath.Join(cfg.RepoRoot, LegacyMatrixRelPath)
		if _, lerr := os.Stat(legacy); lerr == nil {
			matrixPath = legacy
		} else {
			return nil, fmt.Errorf("divergence matrix not found at %s (or legacy %s)", MatrixRelPath, LegacyMatrixRelPath)
		}
	}

	matrixData, err := os.ReadFile(matrixPath)
	if err != nil {
		return nil, fmt.Errorf("read matrix: %w", err)
	}
	matrixContent := string(matrixData)
	matrixIDs := extractMatrixIDs(matrixContent)
	openIDs := extractOpenIDs(matrixContent)

	codeMarkers, err := scanCodeMarkers(cfg.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("scan code markers: %w", err)
	}

	outcome := &Outcome{Pass: true}

	// 1. Every code marker must reference a known matrix ID.
	for id, locs := range codeMarkers {
		if !matrixIDs[id] {
			for _, loc := range locs {
				outcome.Pass = false
				outcome.Violations = append(outcome.Violations,
					fmt.Sprintf("UNKNOWN_ID: %s referenced at %s but not in matrix", id, loc))
			}
		}
	}

	// 2. Every OPEN matrix entry must have a live code marker.
	for id := range openIDs {
		if _, ok := codeMarkers[id]; !ok {
			outcome.Pass = false
			outcome.Violations = append(outcome.Violations,
				fmt.Sprintf("ORPHAN_MATRIX_ENTRY: %s (OPEN status but no code marker found)", id))
		}
	}

	return outcome, nil
}

// extractMatrixIDs returns the set of all LP-NNN ids mentioned anywhere in the
// matrix content (table rows OR inline references).
func extractMatrixIDs(content string) map[string]bool {
	ids := map[string]bool{}
	for _, m := range lpIDRe.FindAllString(content, -1) {
		ids[m] = true
	}
	return ids
}

// extractOpenIDs returns LP-NNN ids whose matrix table row contains "OPEN".
func extractOpenIDs(content string) map[string]bool {
	open := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		if !matrixRowRe.MatchString(line) {
			continue
		}
		if !strings.Contains(line, "OPEN") {
			continue
		}
		id := lpIDRe.FindString(line)
		if id != "" {
			open[id] = true
		}
	}
	return open
}

// scanCodeMarkers walks scanDirs and returns map[LP-NNN][]location.
func scanCodeMarkers(repoRoot string) (map[string][]string, error) {
	markers := map[string][]string{}
	for _, dir := range scanDirs {
		base := filepath.Join(repoRoot, dir)
		if _, err := os.Stat(base); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			// Skip test files: fixtures legitimately contain example LP-NNN
			// markers that are not real LOCAL-ONLY production code paths.
			name := d.Name()
			if strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_test.sh") {
				return nil
			}
			for _, ex := range excludedDirSubstrings {
				if strings.Contains(path, ex) {
					return nil
				}
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil // skip unreadable
			}
			for _, marker := range codeMarkerRe.FindAllString(string(data), -1) {
				id := lpIDRe.FindString(marker)
				rel, _ := filepath.Rel(repoRoot, path)
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
