// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package orchestrationbreaker_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/orchestrationbreaker"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

func TestWorkflowCircuitBreaker_ClosedState_DelegatesToInner(t *testing.T) {
	// Arrange
	inner := &fakeWorkflowSubmitter{}
	cb := orchestrationbreaker.NewCircuitBreakerWorkflowSubmitter(inner, orchestrationbreaker.CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 2,
		RetryInterval:  30 * time.Second,
	}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Act
	id, err := cb.Submit(context.Background(), deploycontracts.WorkflowSpec{BuildID: "b-001"})

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id != "wf-fake-001" {
		t.Errorf("expected wf-fake-001, got %q", id)
	}
	if len(inner.submitted) != 1 {
		t.Errorf("expected 1 submission, got %d", len(inner.submitted))
	}
}

func TestWorkflowCircuitBreaker_OpensAfterThresholdFailures(t *testing.T) {
	// Arrange
	inner := &fakeWorkflowSubmitter{err: fmt.Errorf("connection refused")}
	cb := orchestrationbreaker.NewCircuitBreakerWorkflowSubmitter(inner, orchestrationbreaker.CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 2,
		RetryInterval:  30 * time.Second,
	}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Act — 3 failures to open the circuit
	for i := 0; i < 3; i++ {
		_, _ = cb.Submit(context.Background(), deploycontracts.WorkflowSpec{BuildID: fmt.Sprintf("b-%03d", i)})
	}

	// 4th attempt should be rejected immediately by circuit breaker
	_, err := cb.Submit(context.Background(), deploycontracts.WorkflowSpec{BuildID: "b-003"})

	// Assert
	if err == nil {
		t.Fatal("expected error from open circuit, got nil")
	}
	if err != loopruntimeerrors.ErrPlatformDegraded {
		t.Errorf("expected ErrPlatformDegraded, got %v", err)
	}
}

func TestWorkflowCircuitBreaker_CancelNotProtected(t *testing.T) {
	// Cancel should always delegate regardless of circuit state
	inner := &fakeWorkflowSubmitter{err: fmt.Errorf("connection refused")}
	cb := orchestrationbreaker.NewCircuitBreakerWorkflowSubmitter(inner, orchestrationbreaker.CircuitBreakerConfig{
		OpenThreshold:  1, // open after 1 failure
		CloseThreshold: 1,
		RetryInterval:  1 * time.Hour, // won't recover in this test
	}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Open circuit
	_, _ = cb.Submit(context.Background(), deploycontracts.WorkflowSpec{BuildID: "b-001"})

	// Cancel should still go through
	err := cb.Cancel(context.Background(), "build-acme", "wf-123")
	if err != nil {
		t.Errorf("expected Cancel to pass through, got %v", err)
	}
}

func TestWorkflowCircuitBreaker_GetStatusNotProtected(t *testing.T) {
	// GetStatus should always delegate regardless of circuit state
	inner := &fakeWorkflowSubmitter{err: fmt.Errorf("connection refused")}
	cb := orchestrationbreaker.NewCircuitBreakerWorkflowSubmitter(inner, orchestrationbreaker.CircuitBreakerConfig{
		OpenThreshold:  1,
		CloseThreshold: 1,
		RetryInterval:  1 * time.Hour,
	}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Open circuit
	_, _ = cb.Submit(context.Background(), deploycontracts.WorkflowSpec{BuildID: "b-001"})

	// GetStatus should still go through
	status, err := cb.GetStatus(context.Background(), "build-acme", "wf-123")
	if err != nil {
		t.Errorf("expected GetStatus to pass through, got %v", err)
	}
	if status == nil {
		t.Error("expected non-nil status")
	}
}

func TestWorkflowCircuitBreaker_RecoveryProbe(t *testing.T) {
	// After retryInterval, circuit should allow a recovery probe
	inner := &fakeWorkflowSubmitter{err: fmt.Errorf("connection refused")}
	cb := orchestrationbreaker.NewCircuitBreakerWorkflowSubmitter(inner, orchestrationbreaker.CircuitBreakerConfig{
		OpenThreshold:  1,
		CloseThreshold: 2,
		RetryInterval:  10 * time.Millisecond, // short for testing
	}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Open circuit
	_, _ = cb.Submit(context.Background(), deploycontracts.WorkflowSpec{BuildID: "b-001"})

	// Verify circuit is open
	_, err := cb.Submit(context.Background(), deploycontracts.WorkflowSpec{BuildID: "b-002"})
	if err != loopruntimeerrors.ErrPlatformDegraded {
		t.Fatalf("expected ErrPlatformDegraded, got %v", err)
	}

	time.Sleep(20 * time.Millisecond) // intentional: testing real-time behavior — wait past the 10ms retry interval

	// Fix inner and send recovery probes
	inner.err = nil
	_, err = cb.Submit(context.Background(), deploycontracts.WorkflowSpec{BuildID: "b-003"})
	if err != nil {
		t.Fatalf("expected recovery probe to succeed, got %v", err)
	}

	// Need 2 successes for closeThreshold=2
	_, err = cb.Submit(context.Background(), deploycontracts.WorkflowSpec{BuildID: "b-004"})
	if err != nil {
		t.Fatalf("expected second probe to succeed, got %v", err)
	}

	// Circuit should be closed now — next submit should work
	_, err = cb.Submit(context.Background(), deploycontracts.WorkflowSpec{BuildID: "b-005"})
	if err != nil {
		t.Errorf("expected closed circuit to allow submit, got %v", err)
	}
}
