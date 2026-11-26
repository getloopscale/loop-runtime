// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package repoinvariants

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

// A file in infra/quality/ is read as GATE INPUT. With no reader it looks like
// a contract and is not, and the difference only shows when someone edits it
// believing it changes something.
//
// Measured 2026-08-19 (B-091): of the directory's 12 data files, eleven had 1 to
// 8 readers and ONE had zero — the second orphan baseline of the same family,
// and stale besides: three of its four entries named scripts that no longer
// existed.
//
// Its name is deliberately not written here: this assertion counts readers by
// searching for the file name in code, so naming it in this comment would make
// THIS test the missing reader. The first version did exactly that.
func TestEveryInfraQualityFileHasAReader(t *testing.T) {
	root := qtestutil.MustRepoRoot()
	dir := filepath.Join(root, "infra", "quality")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	var files []string
	for _, e := range ents {
		// The README documents the DIRECTORY and is not gate input. The
		// exemption is declared rather than silent because the name is generic
		// enough to match dozens of files and produce a "reader" that reads
		// nothing.
		if e.IsDir() || e.Name() == "README.md" {
			continue
		}
		files = append(files, e.Name())
	}
	if len(files) == 0 {
		t.Fatal("zero files scanned: an assertion over nothing passes vacuously")
	}

	for _, name := range files {
		out, err := exec.Command("grep", "-rl", "--include=*.go", "--include=*.yml",
			"--include=*.yaml", "--include=*.sh", name, root).Output()
		var exitErr *exec.ExitError
		switch {
		case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
			// grep found nothing: no reader at all.
		case err != nil:
			t.Fatalf("grep for %s could not run: %v", name, err)
		}
		readers := 0
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if l == "" || strings.Contains(l, filepath.Join("infra", "quality")) {
				continue
			}
			// The file doing the counting is not a reader, or any name it cites
			// would satisfy itself.
			if strings.HasSuffix(l, "baseline_readers_test.go") {
				continue
			}
			readers++
		}
		if readers == 0 {
			t.Errorf("%s has no reader outside infra/quality/ — it looks like a contract and is not", name)
		}
	}
}
