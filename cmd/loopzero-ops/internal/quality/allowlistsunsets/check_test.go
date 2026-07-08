// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package allowlistsunsets

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCheck_RealRepoPasses uses the actual repo layout — python3 required.
// If python3 is missing on the test host, the test is skipped so this stays
// portable across CI/local dev.
func TestCheck_RealRepoPasses(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH; skipping (this gate is a driver for the Python script)")
	}
	abs, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, statErr := os.Stat(filepath.Join(abs, gatePath)); statErr == nil {
			outcome, checkErr := Check(Config{RepoRoot: abs})
			if checkErr != nil {
				t.Fatalf("real repo must satisfy fixture invariants + live check. err=%v failures=%v", checkErr, outcome.Failures)
			}
			return
		}
		abs = filepath.Dir(abs)
	}
	t.Skip("could not locate the Python gate within 8 parent dirs")
}

// TestCheck_MissingGateFails ensures the check surfaces a helpful failure
// when the Python script is not present in the fixture repo root.
func TestCheck_MissingGateFails(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	root := t.TempDir()
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for missing gate, got %v", err)
	}
}
