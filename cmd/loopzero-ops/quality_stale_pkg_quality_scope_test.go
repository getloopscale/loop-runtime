// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// RED test for B-014, written to MEASURE and left UNTRACKED.
//
// TestNoStalePkgQualityPath (quality_stale_pkg_quality_path_test.go) roots at
// filepath.Abs(".") — the test binary's own directory, cmd/loopzero-ops. It
// therefore cannot see a stale `pkg/quality/` citation anywhere else in the
// repository, and it passes today for that reason rather than because the tree
// is clean.
//
// This test applies the SAME matcher from the repository root. It fails now.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRootFromHere walks up from cmd/loopzero-ops to the directory holding go.work.
func repoRootFromHere(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, statErr := os.Stat(filepath.Join(dir, "go.work")); statErr == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("go.work not found walking up from cmd/loopzero-ops")
	return ""
}

// collectGoSourcesUnder mirrors collectGoSources, rooted anywhere.
func collectGoSourcesUnder(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // unreadable subtree is not this test's subject
		}
		if info.IsDir() {
			base := info.Name()
			if base == "node_modules" || base == "vendor" || base == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip the guards themselves: both must name the token to detect it.
		if strings.HasSuffix(path, "quality_stale_pkg_quality_path_test.go") ||
			strings.HasSuffix(path, "quality_stale_pkg_quality_scope_test.go") {
			return nil
		}
		b, readErr := os.ReadFile(path) // #nosec G304 -- path from Walk of the repo root
		if readErr != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// TestStalePkgQualityPath_RepoWide is the RED assertion.
func TestStalePkgQualityPath_RepoWide(t *testing.T) {
	root := repoRootFromHere(t)
	sources := collectGoSourcesUnder(t, root)

	// Empty-scan floor: a matcher that enumerated nothing must FAIL, not pass.
	if len(sources) == 0 {
		t.Fatalf("no .go sources found under %s — the guard would pass vacuously", root)
	}
	t.Logf("scanned %d .go files under the repository root", len(sources))

	var hits []string
	for rel, body := range sources {
		for i, line := range strings.Split(body, "\n") {
			if strings.Contains(line, "pkg/quality/") {
				hits = append(hits, rel+":"+itoa(i+1))
			}
		}
	}
	for _, h := range hits {
		t.Errorf("%s cites \"pkg/quality/\", relocated to \"cmd/loopzero-ops/internal/quality/\" at M9 (d43dae086) — invisible to the cmd/loopzero-ops-rooted guard", h)
	}
}

// TestStalePkgQualityPath_RepoWide_PositiveControl proves the widened walk
// actually reaches api/ — a zero from a walk that never got there is not
// evidence of a clean tree.
func TestStalePkgQualityPath_RepoWide_PositiveControl(t *testing.T) {
	root := repoRootFromHere(t)
	sources := collectGoSourcesUnder(t, root)

	var underAPI int
	for rel := range sources {
		if strings.HasPrefix(rel, "api/") {
			underAPI++
		}
	}
	if underAPI == 0 {
		t.Fatalf("positive control FAILED: the walk from %s reached 0 files under api/ — a zero-hit result would prove nothing", root)
	}
	t.Logf("positive control: walk reached %d .go files under api/", underAPI)

	if _, ok := sources["api/internal/correlation/reader.go"]; !ok {
		t.Fatalf("positive control FAILED: api/internal/correlation/reader.go was not collected")
	}
}
