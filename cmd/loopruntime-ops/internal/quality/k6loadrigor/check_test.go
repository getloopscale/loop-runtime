// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package k6loadrigor

import (
	"errors"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

// TestCheck_RealRepoPasses is the primary sanity check: the actual repo
// state must satisfy the invariant post-migration.
func TestCheck_RealRepoPasses(t *testing.T) {
	outcome, err := violationOf(Check(Config{RepoRoot: qtestutil.MustRepoRoot()}))
	if err != nil {
		t.Fatalf("real repo must satisfy invariant. err=%v failures=%v", err, outcome.Failures)
	}
}

// TestCheck_MissingThresholdsFails ensures the check surfaces a helpful
// failure when the target file is absent.
func TestCheck_MissingThresholdsFails(t *testing.T) {
	root := t.TempDir()
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for empty repo, got %v", err)
	}
}
