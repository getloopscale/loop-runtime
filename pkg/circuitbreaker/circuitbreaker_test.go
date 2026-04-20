// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package circuitbreaker

import (
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
	cb := New(Config{Name: "test", OpenThreshold: 1, CloseThreshold: 1, RetryInterval: 10 * time.Millisecond}, nil)
	cb.RecordResult(errTest) // opens

	time.Sleep(15 * time.Millisecond)

	// Should transition to half-open and allow probe
	if err := cb.PreCheck(); err != nil {
		t.Fatalf("after retry interval, should allow probe, got: %v", err)
	}
	if cb.State() != HalfOpen {
		t.Fatalf("expected HalfOpen, got %v", cb.State())
	}
}

func TestBreaker_HalfOpenSuccess_ClosesCircuit(t *testing.T) {
	cb := New(Config{Name: "test", OpenThreshold: 1, CloseThreshold: 1, RetryInterval: 10 * time.Millisecond}, nil)
	cb.RecordResult(errTest)          // opens
	time.Sleep(15 * time.Millisecond) // wait for retry interval
	_ = cb.PreCheck()                 // half-open

	cb.RecordResult(nil) // success → closes
	if cb.State() != Closed {
		t.Fatalf("success in half-open should close circuit, got %v", cb.State())
	}
}

func TestBreaker_HalfOpenFailure_ReOpens(t *testing.T) {
	cb := New(Config{Name: "test", OpenThreshold: 1, CloseThreshold: 1, RetryInterval: 10 * time.Millisecond}, nil)
	cb.RecordResult(errTest)          // opens
	time.Sleep(15 * time.Millisecond) // wait for retry interval
	_ = cb.PreCheck()                 // half-open

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
