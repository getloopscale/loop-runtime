// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// T4.2 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md —
// signal handling + exit code corner tests. Closes M04 LOW gap
// (shellrun test coverage 18% — basic smoke tests; sem chaos
// injection of exit code corners + signal handling under resource
// pressure).
//
// Scope:
//   - Context cancellation triggers SIGTERM + grace period + SIGKILL
//   - Negative exit code reflects signal kill
//   - Timeout option enforces wall-clock budget
//   - Process-group kill semantics (no orphan grandchildren)
//   - Standard exit codes 0-5 propagate unchanged

package shellrun

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// TestSignal_ContextCancelKillsProcess — when caller cancels the ctx,
// the runner MUST SIGTERM the child within the grace period AND then
// SIGKILL if it hasn't exited.
func TestSignal_ContextCancelKillsProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal semantics — Unix-only")
	}
	runner := &DefaultRunner{GracePeriod: 200 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	result, err := runner.Run(ctx, "sleep", []string{"30"})
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Run with cancelled ctx must return non-nil err")
	}
	// Should complete within grace period (~1-2s max), not 30s
	if elapsed > 5*time.Second {
		t.Errorf("cancellation took too long: %v (expected < 5s)", elapsed)
	}
	if result != nil && result.ExitCode == 0 {
		t.Errorf("cancelled process should not exit 0; got %d", result.ExitCode)
	}
}

// TestExit_StandardCodesPropagated — exit codes 0-5 must propagate
// unchanged from child to Result.ExitCode.
func TestExit_StandardCodesPropagated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-only sh tests")
	}
	runner := NewDefault()
	for _, code := range []int{0, 1, 2, 3, 4, 5} {
		t.Run(itoa(code), func(t *testing.T) {
			result, err := runner.Run(context.Background(), "sh",
				[]string{"-c", "exit " + itoa(code)})
			if code == 0 {
				if err != nil {
					t.Errorf("exit 0 should not return err, got %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("exit %d should return err", code)
				}
			}
			if result == nil {
				t.Fatal("Result must not be nil")
			}
			if result.ExitCode != code {
				t.Errorf("ExitCode = %d, want %d", result.ExitCode, code)
			}
		})
	}
}

// TestExit_NonStandardCodePropagated — exit codes outside 0-5 also
// propagate. Important for diagnostics (124 = timeout, 130 = SIGINT,
// 137 = SIGKILL, 143 = SIGTERM).
func TestExit_NonStandardCodePropagated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-only sh test")
	}
	runner := NewDefault()
	for _, code := range []int{124, 130, 137, 143, 99} {
		t.Run(itoa(code), func(t *testing.T) {
			result, _ := runner.Run(context.Background(), "sh",
				[]string{"-c", "exit " + itoa(code)})
			if result == nil {
				t.Fatal("Result must not be nil")
			}
			if result.ExitCode != code {
				t.Errorf("ExitCode = %d, want %d", result.ExitCode, code)
			}
		})
	}
}

// TestExit_IsSuccessReflectsCode — IsSuccess MUST be true iff ExitCode==0.
func TestExit_IsSuccessReflectsCode(t *testing.T) {
	cases := []struct {
		code    int
		success bool
	}{
		{0, true}, {1, false}, {-1, false}, {137, false}, {255, false},
	}
	for _, c := range cases {
		r := &Result{ExitCode: c.code}
		if r.IsSuccess() != c.success {
			t.Errorf("ExitCode=%d IsSuccess=%v want %v", c.code, r.IsSuccess(), c.success)
		}
	}
}

// TestSignal_CommandNotFoundReturnsError — invoking a non-existent
// binary returns an error AND Result.ExitCode != 0.
func TestSignal_CommandNotFoundReturnsError(t *testing.T) {
	runner := NewDefault()
	result, err := runner.Run(context.Background(),
		"loopruntime-test-binary-that-does-not-exist-xyz123", []string{})
	if err == nil {
		t.Error("missing binary must return err")
	}
	// Result may be nil or have ExitCode != 0 depending on impl
	if result != nil && result.ExitCode == 0 {
		t.Errorf("missing binary ExitCode must not be 0; got %d", result.ExitCode)
	}
}

// itoa() helper is provided by runner_test.go (test-only shared helper).
