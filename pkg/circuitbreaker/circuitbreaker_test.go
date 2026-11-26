// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package circuitbreaker

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errTest = errors.New("test error")

func TestBreaker_StartsClosedAllowsCalls(t *testing.T) {
	cb := New(Config{Name: "test", OpenThreshold: 3, CloseThreshold: 1, RetryInterval: time.Second}, nil)

	if err := cb.PreCheck(); err != nil {
		t.Fatalf("closed circuit should allow calls, got: %v", err)
	}
	if cb.State() != Closed {
		t.Fatalf("expected Closed, got %v", cb.State())
	}
}

func TestBreaker_OpensAfterThresholdFailures(t *testing.T) {
	var transitions []State
	cb := New(Config{Name: "test", OpenThreshold: 3, CloseThreshold: 1, RetryInterval: time.Second},
		func(name string, from, to State) { transitions = append(transitions, to) })

	// 2 failures: still closed
	cb.RecordResult(errTest)
	cb.RecordResult(errTest)
	if cb.State() != Closed {
		t.Fatalf("2 failures (threshold 3) should keep Closed, got %v", cb.State())
	}

	// 3rd failure: opens
	cb.RecordResult(errTest)
	if cb.State() != Open {
		t.Fatalf("3 failures should Open circuit, got %v", cb.State())
	}
	if len(transitions) != 1 || transitions[0] != Open {
		t.Fatalf("expected transition to Open, got %v", transitions)
	}
}

func TestBreaker_OpenRejectsCalls(t *testing.T) {
	cb := New(Config{Name: "test", OpenThreshold: 1, CloseThreshold: 1, RetryInterval: time.Hour}, nil)
	cb.RecordResult(errTest) // opens

	err := cb.PreCheck()
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("open circuit should return ErrCircuitOpen, got: %v", err)
	}
}

func TestBreaker_TransitionsToHalfOpenAfterRetryInterval(t *testing.T) {
	cb, clock := openBreaker(t, 1, nil)

	clock.advance(time.Minute)

	// Should transition to half-open and allow the probe
	if err := cb.PreCheck(); err != nil {
		t.Fatalf("after retry interval, should allow probe, got: %v", err)
	}
	if cb.State() != HalfOpen {
		t.Fatalf("expected HalfOpen, got %v", cb.State())
	}
}

func TestBreaker_HalfOpenSuccess_ClosesCircuit(t *testing.T) {
	cb, clock := openBreaker(t, 1, nil)
	clock.advance(time.Minute)
	if err := cb.PreCheck(); err != nil { // half-open probe
		t.Fatalf("probe must be admitted: %v", err)
	}

	cb.RecordResult(nil) // success → closes
	if cb.State() != Closed {
		t.Fatalf("success in half-open should close circuit, got %v", cb.State())
	}
}

func TestBreaker_HalfOpenFailure_ReOpens(t *testing.T) {
	cb, clock := openBreaker(t, 1, nil)
	clock.advance(time.Minute)
	if err := cb.PreCheck(); err != nil { // half-open probe
		t.Fatalf("probe must be admitted: %v", err)
	}

	cb.RecordResult(errTest) // failure → re-opens
	if cb.State() != Open {
		t.Fatalf("failure in half-open should re-open, got %v", cb.State())
	}
}

func TestBreaker_SuccessResetsFailureCount(t *testing.T) {
	cb := New(Config{Name: "test", OpenThreshold: 3, CloseThreshold: 1, RetryInterval: time.Second}, nil)

	cb.RecordResult(errTest) // failure 1
	cb.RecordResult(errTest) // failure 2
	cb.RecordResult(nil)     // success resets count
	cb.RecordResult(errTest) // failure 1 (reset)
	cb.RecordResult(errTest) // failure 2

	if cb.State() != Closed {
		t.Fatalf("success should reset failure count, circuit should be Closed, got %v", cb.State())
	}
}

func TestBreaker_DefaultConfig(t *testing.T) {
	cb := New(Config{Name: "test"}, nil)

	// Should use defaults: OpenThreshold=5, CloseThreshold=1, RetryInterval=120s
	for i := 0; i < 4; i++ {
		cb.RecordResult(errTest)
	}
	if cb.State() != Closed {
		t.Fatalf("4 failures with default threshold 5 should stay Closed, got %v", cb.State())
	}
	cb.RecordResult(errTest)
	if cb.State() != Open {
		t.Fatalf("5 failures with default threshold 5 should Open, got %v", cb.State())
	}
}

func TestBreaker_StateString(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{Closed, "closed"},
		{Open, "open"},
		{HalfOpen, "half-open"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// T2.1 — Execute[T] generic + context-aware (ADR-0031 amended).
// ---------------------------------------------------------------------------

func TestExecute_ReturnsTypedResultOnSuccess(t *testing.T) {
	cb := New(Config{Name: "exec-typed", OpenThreshold: 3, RetryInterval: time.Second}, nil)
	got, err := Execute(t.Context(), cb, func(ctx context.Context) (int, error) {
		return 42, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 42 {
		t.Errorf("Execute returned %d, want 42", got)
	}
	if cb.State() != Closed {
		t.Errorf("CB should remain Closed after success; got %v", cb.State())
	}
}

func TestExecute_PropagatesContextCancellation(t *testing.T) {
	cb := New(Config{Name: "exec-ctx", OpenThreshold: 3, RetryInterval: time.Second}, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // pre-cancel before Execute

	var fnRanWithCancelledCtx bool
	_, err := Execute(ctx, cb, func(ctx context.Context) (string, error) {
		if ctx.Err() != nil {
			fnRanWithCancelledCtx = true
			return "", ctx.Err()
		}
		return "ok", nil
	})
	if err == nil {
		t.Fatal("expected context.Canceled error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if !fnRanWithCancelledCtx {
		t.Error("fn should observe cancelled ctx")
	}
}

func TestExecute_DefaultExcludesContextCanceledFromFailures(t *testing.T) {
	cb := New(Config{Name: "exec-iexc", OpenThreshold: 2, RetryInterval: time.Second}, nil)

	// Simulate 5 cancellations (would saturate w/o exclusion).
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, _ = Execute(ctx, cb, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, ctx.Err()
		})
	}
	if cb.State() != Closed {
		t.Errorf("default IsExcluded must drop context.Canceled; CB state=%v", cb.State())
	}
}

func TestExecute_DefaultExcludesDeadlineExceeded(t *testing.T) {
	cb := New(Config{Name: "exec-deadline", OpenThreshold: 2, RetryInterval: time.Second}, nil)
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now())
		_, _ = Execute(ctx, cb, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, context.DeadlineExceeded
		})
		cancel()
	}
	if cb.State() != Closed {
		t.Errorf("default IsExcluded must drop context.DeadlineExceeded; CB state=%v", cb.State())
	}
}

func TestExecute_GenuineFailuresStillOpenCircuit(t *testing.T) {
	cb := New(Config{Name: "exec-fails", OpenThreshold: 3, RetryInterval: time.Second}, nil)
	for i := 0; i < 3; i++ {
		_, _ = Execute(t.Context(), cb, func(ctx context.Context) (int, error) {
			return 0, errTest
		})
	}
	if cb.State() != Open {
		t.Errorf("3 genuine failures should open CB; got %v", cb.State())
	}
}

func TestExecute_RejectsWhenCircuitIsOpen(t *testing.T) {
	cb := New(Config{Name: "exec-reject", OpenThreshold: 1, RetryInterval: time.Hour}, nil)
	// Force open
	_, _ = Execute(t.Context(), cb, func(ctx context.Context) (int, error) { return 0, errTest })
	if cb.State() != Open {
		t.Fatalf("CB must be Open before next Execute, got %v", cb.State())
	}

	called := false
	_, err := Execute(t.Context(), cb, func(ctx context.Context) (int, error) {
		called = true
		return 1, nil
	})
	if err == nil || !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	if called {
		t.Error("fn must NOT run when CB is Open")
	}
}

func TestExecute_HonorsCustomIsExcluded(t *testing.T) {
	custom := errors.New("custom-soft-error")
	cb := New(Config{
		Name:          "exec-custom",
		OpenThreshold: 2,
		RetryInterval: time.Second,
		IsExcluded:    func(err error) bool { return errors.Is(err, custom) },
	}, nil)

	for i := 0; i < 5; i++ {
		_, _ = Execute(t.Context(), cb, func(ctx context.Context) (int, error) { return 0, custom })
	}
	if cb.State() != Closed {
		t.Errorf("custom IsExcluded must drop the error; CB state=%v", cb.State())
	}
}
