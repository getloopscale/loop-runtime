// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domainsinkpurity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qtestutil"
)

// modulePrefix is the only prefix the watched paths use. Deriving the directory
// from the import path is valid only while this holds, and the test's first
// assertion is precisely that it still does.
const modulePrefix = "github.com/getloopscale/loop-runtime/"

// This gate watches by IMPORT PATH. A path that stops existing — a package
// moved, renamed or deleted — produces no error: the key simply stops matching,
// the package drops out of the watch and the gate stays green over less than it
// promises.
//
// This is not a hypothesis. The map's own doc-comment records that it happened:
// "the first implementation did not watch them at all — three public references
// crossed with the gate green". What was missing was this check, and without it
// the next package move repeats the episode.
func TestWatchedPathsExistInTheTree(t *testing.T) {
	root := qtestutil.Root(t)
	if len(watchedImportPaths) == 0 {
		t.Fatal("empty watch map: a gate that watches nothing passes vacuously")
	}
	for path := range watchedImportPaths {
		if !strings.HasPrefix(path, modulePrefix) {
			t.Errorf("%q does not start with %q — the translation to a directory no longer holds",
				path, modulePrefix)
			continue
		}
		dir := filepath.Join(root, strings.TrimPrefix(path, modulePrefix))
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			t.Errorf("the gate watches %q, but %s does not exist — the package dropped out of the watch "+
				"silently and green no longer means what it promises", path, dir)
		}
	}
}

// The sink directory has the same failure mode, and it is worse: without it the
// gate has nothing to read.
func TestSinkDirectoryExists(t *testing.T) {
	dir := filepath.Join(qtestutil.Root(t), sinkDir)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("sinkDir points at %s, which does not exist", dir)
	}
}
