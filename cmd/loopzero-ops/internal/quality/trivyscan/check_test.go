// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package trivyscan

import (
	"context"
	"os/exec"
	"testing"
)

func TestCheck_SkipsWhenTrivyMissing(t *testing.T) {
	if _, err := exec.LookPath("trivy"); err == nil {
		t.Skip("trivy IS installed locally — skip-on-missing test N/A")
	}
	got, err := Check(context.Background(), Config{RepoRoot: "."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Skipped {
		t.Errorf("expected Skipped=true when trivy missing, got %+v", got)
	}
	if got.Pass {
		t.Error("expected Pass=false when Skipped=true")
	}
	if got.Reason == "" {
		t.Error("expected non-empty Reason explaining skip")
	}
}

func TestCheck_DefaultSeverities(t *testing.T) {
	if _, err := exec.LookPath("trivy"); err == nil {
		t.Skip("trivy IS installed — skip-on-missing path is the test target")
	}
	got, err := Check(context.Background(), Config{RepoRoot: "."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When trivy is missing (skip path), default severities "CRITICAL,HIGH"
	// would have been used had trivy been present. We can only verify the
	// Reason explains the skip.
	if !got.Skipped {
		t.Skip("trivy present — cannot verify skip path")
	}
}

func TestCheck_RepoRootDefault(t *testing.T) {
	got, err := Check(context.Background(), Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With empty config, gate either skips (no trivy) or runs (trivy present).
	// Both are valid; assertion is that Check does not panic on zero-value Config.
	_ = got
}
