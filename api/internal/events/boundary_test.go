// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package events_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This package used to be `pkg/events` — 329 lines of PUBLIC API that only the
// `api` module ever consumed. Under `api/internal/`, Go's own internal rule
// makes importing it from another module impossible: not a convention, not a
// gate, a compile error.
//
// That is the whole point of the move (B-043), so it is asserted by actually
// compiling the violation rather than by trusting the rule.
//
// # It builds in a temp dir, never inside another module
//
// The first version wrote the probe into `operators/internal/` and relied on
// t.Cleanup to remove it. Two failure modes made that wrong, and both were
// raised by review:
//
//   - t.Cleanup does not run on abnormal termination (the -timeout watchdog
//     panic, SIGINT, SIGKILL). A leftover deliberately-uncompilable file is not
//     gitignored, so `cd operators && go build ./...` would break permanently and
//     nobody would connect it to a test in `api`.
//   - The Tiltfile watches `operators/` for the four operator images. Creating a
//     file there while `tilt up` runs — the canonical day-to-day loop — can turn
//     those builds red non-deterministically.
//
// A throwaway module in t.TempDir() with its own go.work reproduces the exact
// same compiler check and touches nothing anyone else is watching.
func TestAnotherModuleCannotImportThisPackage(t *testing.T) {
	repo := repoRoot(t)
	dir := t.TempDir()

	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module probe\n\ngo 1.26.1\n")
	write("go.work", "go 1.26.11\n\nuse (\n\t.\n\t"+filepath.Join(repo, "api")+"\n)\n")
	write("probe.go", "package probe\n\nimport _ \"github.com/getloopscale/loopzero/api/internal/events\"\n")

	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatalf("a foreign module COMPILED an import of api/internal/events — the internal boundary is not holding, and the whole reason this package left pkg/ is gone:\n%s", out)
	}
	// Without this second check ANY build failure would read as "the boundary
	// held", which is how a test like this becomes theatre. Falsified: with the
	// package back under a public path, the build fails for a different reason
	// and this line is what catches the difference.
	if !strings.Contains(string(out), "internal package") {
		t.Errorf("the build failed for some reason OTHER than the internal rule, so this proves nothing about the boundary:\n%s", out)
	}
}

// repoRoot walks up to go.work. Not finding it is a FAILURE, never a skip — a
// skip would make the boundary assertion pass by not running.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	start := dir
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("go.work not found within 8 levels above %s — the boundary assertion cannot run, and skipping it would report green while measuring nothing", start)
	return ""
}
