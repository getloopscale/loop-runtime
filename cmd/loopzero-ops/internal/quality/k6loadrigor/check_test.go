// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package k6loadrigor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestCheck_RealRepoPasses is the primary sanity check: the actual repo
// state must satisfy the invariant post-migration.
func TestCheck_RealRepoPasses(t *testing.T) {
	abs, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, statErr := os.Stat(filepath.Join(abs, thresholdsPath)); statErr == nil {
			outcome, checkErr := Check(Config{RepoRoot: abs})
			if checkErr != nil {
				t.Fatalf("real repo must satisfy invariant. err=%v failures=%v", checkErr, outcome.Failures)
			}
			return
		}
		abs = filepath.Dir(abs)
	}
	t.Skip("could not locate k6 thresholds within 8 parent dirs; skipping")
}

// TestCheck_MissingThresholdsFails ensures the check surfaces a helpful
// failure when the target file is absent.
func TestCheck_MissingThresholdsFails(t *testing.T) {
	root := t.TempDir()
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for empty repo, got %v", err)
	}
}
