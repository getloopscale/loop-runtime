// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package orchestrationbreaker

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// --- Test doubles ---

type fakeWorkflowSubmitter struct {
	submitErr error
	cancelErr error
	statusErr error
	status    *deploycontracts.WorkflowStatus
	submitted int
}

func (f *fakeWorkflowSubmitter) Submit(_ context.Context, _ deploycontracts.WorkflowSpec) (string, error) {
	f.submitted++
	return "wf-123", f.submitErr
}
func (f *fakeWorkflowSubmitter) Cancel(_ context.Context, _, _ string) error {
	return f.cancelErr
}
func (f *fakeWorkflowSubmitter) GetStatus(_ context.Context, _, _ string) (*deploycontracts.WorkflowStatus, error) {
	return f.status, f.statusErr
}

// --- Tests ---

func TestWorkflowCircuitBreaker_Closed_PassesThrough(t *testing.T) {
	inner := &fakeWorkflowSubmitter{}
	cb := NewCircuitBreakerWorkflowSubmitter(inner, CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 1,
		RetryInterval:  time.Minute,
	}, slog.Default())

	id, err := cb.Submit(context.Background(), deploycontracts.WorkflowSpec{BuildID: "b-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "wf-123" {
		t.Errorf("expected wf-123, got %q", id)
	}
	if inner.submitted != 1 {
		t.Errorf("expected 1 submission, got %d", inner.submitted)
	}
}

func TestWorkflowCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	inner := &fakeWorkflowSubmitter{submitErr: fmt.Errorf("connection refused")}
	cb := NewCircuitBreakerWorkflowSubmitter(inner, CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 1,
		RetryInterval:  time.Minute,
	}, slog.Default())

	// 3 failures should open the circuit
	for i := 0; i < 3; i++ {
		_, _ = cb.Submit(context.Background(), deploycontracts.WorkflowSpec{})
	}

	// 4th call should be rejected immediately
	_, err := cb.Submit(context.Background(), deploycontracts.WorkflowSpec{})
	if err != loopzeroerrors.ErrPlatformDegraded {
		t.Errorf("expected ErrPlatformDegraded, got %v", err)
	}
	if inner.submitted != 3 {
		t.Errorf("expected 3 submissions (not 4), got %d", inner.submitted)
	}
}

func TestWorkflowCircuitBreaker_Cancel_NotProtected(t *testing.T) {
	inner := &fakeWorkflowSubmitter{submitErr: fmt.Errorf("down")}
	cb := NewCircuitBreakerWorkflowSubmitter(inner, CircuitBreakerConfig{
		OpenThreshold:  1,
		CloseThreshold: 1,
		RetryInterval:  time.Minute,
	}, slog.Default())

	// Open the circuit
	_, _ = cb.Submit(context.Background(), deploycontracts.WorkflowSpec{})

	// Cancel should still pass through (not circuit-breaker protected)
	err := cb.Cancel(context.Background(), "ns", "wf-1")
	if err != nil {
		t.Fatalf("cancel should not be circuit-breaker protected: %v", err)
	}
}

func TestWorkflowCircuitBreaker_GetStatus_NotProtected(t *testing.T) {
	inner := &fakeWorkflowSubmitter{
		submitErr: fmt.Errorf("down"),
		status:    &deploycontracts.WorkflowStatus{Phase: "Running"},
	}
	cb := NewCircuitBreakerWorkflowSubmitter(inner, CircuitBreakerConfig{
		OpenThreshold:  1,
		CloseThreshold: 1,
		RetryInterval:  time.Minute,
	}, slog.Default())

	// Open the circuit
	_, _ = cb.Submit(context.Background(), deploycontracts.WorkflowSpec{})

	// GetStatus should still pass through
	status, err := cb.GetStatus(context.Background(), "ns", "wf-1")
	if err != nil {
		t.Fatalf("GetStatus should not be circuit-breaker protected: %v", err)
	}
	if status.Phase != "Running" {
		t.Errorf("expected Running phase, got %q", status.Phase)
	}
}

func TestWorkflowCircuitBreaker_RecoveryAfterRetryInterval(t *testing.T) {
	inner := &fakeWorkflowSubmitter{submitErr: fmt.Errorf("down")}
	cb := NewCircuitBreakerWorkflowSubmitter(inner, CircuitBreakerConfig{
		OpenThreshold:  1,
		CloseThreshold: 1,
		RetryInterval:  1 * time.Millisecond, // very short for testing
	}, slog.Default())

	// Open the circuit
	_, _ = cb.Submit(context.Background(), deploycontracts.WorkflowSpec{})

	// Wait for retry interval
	time.Sleep(5 * time.Millisecond)

	// Inner now succeeds
	inner.submitErr = nil

	// Should allow a recovery probe (half-open → closed)
	_, err := cb.Submit(context.Background(), deploycontracts.WorkflowSpec{})
	if err != nil {
		t.Fatalf("expected successful recovery probe, got: %v", err)
	}

	// Circuit should now be closed, further calls should work
	_, err = cb.Submit(context.Background(), deploycontracts.WorkflowSpec{})
	if err != nil {
		t.Fatalf("expected success after circuit closed, got: %v", err)
	}
}
