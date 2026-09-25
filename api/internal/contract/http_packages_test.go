// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package contract

import (
	"os"
	"path/filepath"
	"testing"
)

// handlerDirs returns the directories that hold HTTP handlers: every
// `<feature>/<name>http` package of api/internal (docs/decisions.md, 2026-09-25),
// plus the legacy routes tree while it still holds a family. Finding none fails
// the test: a scan over nothing would pass over everything.
func handlerDirs(t *testing.T) []string {
	t.Helper()
	dirs, err := filepath.Glob("../*/*http")
	if err != nil {
		t.Fatalf("globbing handler packages: %v", err)
	}
	var out []string
	for _, d := range dirs {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			out = append(out, d)
		}
	}
	if info, err := os.Stat("../routes"); err == nil && info.IsDir() {
		out = append(out, "../routes")
	}
	if len(out) == 0 {
		t.Fatal("no HTTP handler package found under api/internal — the scan would read nothing")
	}
	return out
}
