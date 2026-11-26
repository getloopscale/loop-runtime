// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package cleanroom

import (
	"context"
	"strings"
	"testing"
	"time"
)

// T3.1 helper-level hermetic tests. Validates Options + Outcome + Run wiring
// without invoking the actual k3d clusters. The orchestrator's behavior under
// LIVE conditions is exercised by the Ginkgo spec (cleanroom_test.go) gated
// behind build tag `live`.

func TestDefaultOptions_SetsCanonicalDefaults(t *testing.T) {
	opts := DefaultOptions("/repo/loop-runtime")
	if opts.RepoRoot != "/repo/loop-runtime" {
		t.Errorf("RepoRoot = %q, want /repo/loop-runtime", opts.RepoRoot)
	}
	if opts.TaskCmd != "task" {
		t.Errorf("TaskCmd = %q, want task", opts.TaskCmd)
	}
	if opts.DoctorTimeout != 10*time.Minute {
		t.Errorf("DoctorTimeout = %v, want 10m", opts.DoctorTimeout)
	}
	if opts.TiltBootWait != 240*time.Second {
		t.Errorf("TiltBootWait = %v, want 240s (per T6.2 fix)", opts.TiltBootWait)
	}
}

func TestRun_RejectsEmptyRepoRoot(t *testing.T) {
	_, err := Run(context.Background(), Options{})
	if err == nil {
		t.Fatal("Run with empty RepoRoot should error")
	}
	if !strings.Contains(err.Error(), "RepoRoot is required") {
		t.Errorf("error = %v, want 'RepoRoot is required'", err)
	}
}

func TestRun_SkipCleanroomAndSkipDoctor_StillReturnsOutcome(t *testing.T) {
	// When BOTH SkipCleanroom and SkipDoctor are true AND SkipBaseline=true,
	// Run has nothing to do but should still return a valid Outcome.
	opts := DefaultOptions("/tmp/fake-repo")
	opts.SkipCleanroom = true
	opts.SkipDoctor = true
	opts.SkipBaseline = true

	outcome, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if outcome == nil {
		t.Fatal("Outcome is nil")
	}
	if len(outcome.Phases) != 0 {
		t.Errorf("len(Phases) = %d, want 0 (all skipped)", len(outcome.Phases))
	}
	if !outcome.OK {
		t.Errorf("OK = false, want true (all skipped is not a failure)")
	}
}

func TestRunPhase_CapturesErrorAndDuration(t *testing.T) {
	// runPhase is an internal helper but accessible from same package test.
	pr := runPhase(context.Background(), "test phase", func() error {
		// intentional-sleep: poll-until-state-transition (M23-T3.2 follow-up: candidate for PollUntil replacement)
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	if !pr.OK {
		t.Errorf("OK = false, want true")
	}
	if pr.Duration < 10*time.Millisecond {
		t.Errorf("Duration = %v, want >= 10ms", pr.Duration)
	}
	if pr.Phase != "test phase" {
		t.Errorf("Phase = %q, want 'test phase'", pr.Phase)
	}
}

func TestRunPhase_PropagatesError(t *testing.T) {
	pr := runPhase(context.Background(), "failing phase", func() error {
		return &fakeErr{msg: "boom"}
	})
	if pr.OK {
		t.Errorf("OK = true, want false on error")
	}
	if pr.Detail != "boom" {
		t.Errorf("Detail = %q, want 'boom'", pr.Detail)
	}
}

type fakeErr struct{ msg string }

func (e *fakeErr) Error() string { return e.msg }
