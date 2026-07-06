// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package qtestutil provides location-independent repo-root resolution for the
// quality gate tests.
//
// M9 Step 4 (externalize scanned paths): gate tests used to read repo files via
// a hardcoded "../../../<path>" offset relative to the gate package's on-disk
// location. That offset broke the moment the gate suite relocated from
// pkg/quality to cmd/loopzero-ops/internal/quality (repo root went from 3 to 5
// levels up). RepoRoot walks up from the current working directory until it
// finds go.work (the monorepo root marker), so a gate test resolves repo files
// independent of where its package lives — the scan root is externalized, not
// baked into a relative-path count.
package qtestutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// RepoRoot returns the absolute monorepo root by walking up from the current
// working directory until it finds go.work. Returns a typed error naming the
// failure (never a silent wrong path).
func RepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("qtestutil.RepoRoot: getwd: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("qtestutil.RepoRoot: go.work not found walking up from working dir")
		}
		dir = parent
	}
}

// MustRepoRoot is RepoRoot for package-level test vars; it panics on failure
// (a test binary that cannot locate the repo root cannot meaningfully run).
func MustRepoRoot() string {
	root, err := RepoRoot()
	if err != nil {
		panic(err)
	}
	return root
}
