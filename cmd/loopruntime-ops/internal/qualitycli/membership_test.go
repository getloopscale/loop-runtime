// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `loopruntime-ops quality --list` is the gate inventory oracle, and wiring_test.go
// requires every REGISTERED gate to be wired or exempted. Nothing reached a
// package that lives under internal/quality/ and was never registered: it does
// not appear in the oracle, is not held to wiring, and still wears the
// directory's name.
//
// Measured 2026-08-18 (B-066): 7 of 148 directories were not imported by the
// registry. Four were legitimate helpers imported by siblings (baseline,
// coverageparse, qgshell, qtestutil); three had zero importers and verified
// phases of a finished plan. The signal that separates the two is the same: a
// helper is imported by a sibling, and a package with neither a registration
// nor an importer does not belong in the directory.
func TestEveryQualityPackageIsAGateOrAHelper(t *testing.T) {
	const qualityRoot = "../quality"
	ents, err := os.ReadDir(qualityRoot)
	if err != nil {
		t.Fatalf("read %s: %v", qualityRoot, err)
	}
	var dirs []string
	for _, e := range ents {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 0 {
		t.Fatal("zero directories scanned: a check over nothing passes vacuously")
	}

	adapters := readSources(t, "*.go")
	for _, d := range dirs {
		imp := "quality/" + d + "\""
		if strings.Contains(adapters, imp) {
			continue
		}
		helper := false
		for _, other := range dirs {
			if other != d && strings.Contains(readSources(t, filepath.Join(qualityRoot, other, "*.go")), imp) {
				helper = true
				break
			}
		}
		if !helper {
			t.Errorf("internal/quality/%s is neither a registered gate nor a helper imported by a sibling — "+
				"it wears the directory's name without belonging to it, and `--list` cannot enumerate it", d)
		}
	}
}

// readSources concatenates every file matching pattern.
func readSources(t *testing.T, pattern string) string {
	t.Helper()
	files, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	var b strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		b.Write(data)
	}
	return b.String()
}
