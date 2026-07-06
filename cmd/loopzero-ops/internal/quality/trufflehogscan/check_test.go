// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package trufflehogscan

import (
	"context"
	"os/exec"
	"testing"
)

func TestCheck_SkipsWhenTrufflehogMissing(t *testing.T) {
	if _, err := exec.LookPath("trufflehog"); err == nil {
		t.Skip("trufflehog IS installed locally — skip-on-missing test N/A")
	}
	got, err := Check(context.Background(), Config{RepoRoot: "."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Skipped {
		t.Errorf("expected Skipped=true when trufflehog missing, got %+v", got)
	}
	if got.Pass {
		t.Error("expected Pass=false when Skipped=true")
	}
	if got.Reason == "" {
		t.Error("expected non-empty Reason explaining skip")
	}
}

func TestCheck_OnlyVerifiedFlag(t *testing.T) {
	got, err := Check(context.Background(), Config{RepoRoot: ".", OnlyVerified: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Just verifying Check accepts the OnlyVerified=true flag without
	// panic. Actual verification semantics tested in LIVE drill (T4.7
	// validation under chosen CI platform per T0.2).
	_ = got
}

func TestCheck_RepoRootDefault(t *testing.T) {
	got, err := Check(context.Background(), Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = got
}
