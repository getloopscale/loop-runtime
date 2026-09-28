// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package orchestrationbreaker

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// --- Test doubles ---

type fakeSyncClientCB struct {
	triggerErr error
	waitErr    error
	triggered  int
}

func (f *fakeSyncClientCB) TriggerSync(_ context.Context, _ string) error {
	f.triggered++
	return f.triggerErr
}
func (f *fakeSyncClientCB) WaitForSync(_ context.Context, _ string, _ time.Duration, _ []string) error {
	return f.waitErr
}

// Sprint B T1.1 (ADR D1): stub methods to satisfy the widened SyncClient
// interface. Circuit-breaker tests exercise only Trigger/Wait error paths;
// the recovery methods (ForceReconcile, GetSyncRevision, TerminateOperation,
// GetAppStatus) bypass the breaker and are covered by the passthrough tests
// below.
func (f *fakeSyncClientCB) ForceReconcile(_ context.Context, _ string) error {
	return f.waitErr
}
func (f *fakeSyncClientCB) GetSyncRevision(_ context.Context, _ string) (string, error) {
	return "", f.waitErr
}
func (f *fakeSyncClientCB) TerminateOperation(_ context.Context, _ string) error {
	return f.waitErr
}
func (f *fakeSyncClientCB) GetAppStatus(_ context.Context, _ string, _ []string) (bool, bool, error) {
	return true, true, f.waitErr
}

// --- Tests ---

func TestPGCircuitBreaker_Closed_PassesThrough(t *testing.T) {
	inner := &fakeSyncClientCB{}
	cb := NewCircuitBreakerSyncClient(inner, CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 1,
		RetryInterval:  time.Minute,
	}, slog.Default())

	err := cb.TriggerSync(context.Background(), "app-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inner.triggered != 1 {
		t.Errorf("expected 1 trigger, got %d", inner.triggered)
	}
}

func TestPGCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	inner := &fakeSyncClientCB{triggerErr: fmt.Errorf("argocd down")}
	cb := NewCircuitBreakerSyncClient(inner, CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 1,
		RetryInterval:  time.Minute,
	}, slog.Default())

	// 3 failures to open
	for i := 0; i < 3; i++ {
		_ = cb.TriggerSync(context.Background(), "app-1")
	}

	// 4th should be rejected
	err := cb.TriggerSync(context.Background(), "app-1")
	if err != loopzeroerrors.ErrPlatformDegraded {
		t.Errorf("expected ErrPlatformDegraded, got %v", err)
	}
	if inner.triggered != 3 {
		t.Errorf("expected 3 triggers, got %d", inner.triggered)
	}
}

func TestPGCircuitBreaker_AuthFailure_NotCounted(t *testing.T) {
	inner := &fakeSyncClientCB{triggerErr: loopzeroerrors.ErrDeployServiceAuthFailed}
	cb := NewCircuitBreakerSyncClient(inner, CircuitBreakerConfig{
		OpenThreshold:  2,
		CloseThreshold: 1,
		RetryInterval:  time.Minute,
	}, slog.Default())

	// Auth failures should not count toward threshold
	for i := 0; i < 5; i++ {
		_ = cb.TriggerSync(context.Background(), "app-1")
	}

	// Circuit should still be closed
	inner.triggerErr = nil
	err := cb.TriggerSync(context.Background(), "app-1")
	if err != nil {
		t.Errorf("expected success (circuit should be closed), got %v", err)
	}
}

func TestPGCircuitBreaker_SyncOperationInProgress_NotCounted(t *testing.T) {
	inner := &fakeSyncClientCB{triggerErr: loopzeroerrors.ErrSyncOperationInProgress}
	cb := NewCircuitBreakerSyncClient(inner, CircuitBreakerConfig{
		OpenThreshold:  2,
		CloseThreshold: 1,
		RetryInterval:  time.Minute,
	}, slog.Default())

	// 409 errors should not count toward threshold
	for i := 0; i < 5; i++ {
		_ = cb.TriggerSync(context.Background(), "app-1")
	}

	// Circuit should still be closed
	inner.triggerErr = nil
	err := cb.TriggerSync(context.Background(), "app-1")
	if err != nil {
		t.Errorf("expected success (circuit should be closed), got %v", err)
	}
}

func TestPGCircuitBreaker_WaitForSync_Protected(t *testing.T) {
	inner := &fakeSyncClientCB{waitErr: fmt.Errorf("argocd down")}
	cb := NewCircuitBreakerSyncClient(inner, CircuitBreakerConfig{
		OpenThreshold:  1,
		CloseThreshold: 1,
		RetryInterval:  time.Minute,
	}, slog.Default())

	// Open via WaitForSync failure
	_ = cb.WaitForSync(context.Background(), "app-1", time.Second, nil)

	// Should be rejected
	err := cb.WaitForSync(context.Background(), "app-1", time.Second, nil)
	if err != loopzeroerrors.ErrPlatformDegraded {
		t.Errorf("expected ErrPlatformDegraded, got %v", err)
	}
}

func TestPGCircuitBreaker_Recovery(t *testing.T) {
	inner := &fakeSyncClientCB{triggerErr: fmt.Errorf("down")}
	cb := NewCircuitBreakerSyncClient(inner, CircuitBreakerConfig{
		OpenThreshold:  1,
		CloseThreshold: 1,
		RetryInterval:  1 * time.Millisecond,
	}, slog.Default())

	// Open the circuit
	_ = cb.TriggerSync(context.Background(), "app-1")

	// Wait for retry interval
	time.Sleep(5 * time.Millisecond)

	// Inner now succeeds
	inner.triggerErr = nil

	err := cb.TriggerSync(context.Background(), "app-1")
	if err != nil {
		t.Fatalf("expected success on recovery probe, got: %v", err)
	}
}

func TestPGCircuitBreaker_HalfOpen_RecoveryFails_ReopensCircuit(t *testing.T) {
	inner := &fakeSyncClientCB{triggerErr: fmt.Errorf("still down")}
	cb := NewCircuitBreakerSyncClient(inner, CircuitBreakerConfig{
		OpenThreshold:  1,
		CloseThreshold: 1,
		RetryInterval:  1 * time.Millisecond,
	}, slog.Default())

	// Open the circuit
	_ = cb.TriggerSync(context.Background(), "app-1")

	// Wait for retry interval
	time.Sleep(5 * time.Millisecond)

	// Recovery probe fails (inner still returns error)
	_ = cb.TriggerSync(context.Background(), "app-1")

	// Circuit should be back to open
	err := cb.TriggerSync(context.Background(), "app-1")
	if err != loopzeroerrors.ErrPlatformDegraded {
		t.Errorf("expected ErrPlatformDegraded after failed recovery, got %v", err)
	}
}

func TestPGCircuitBreaker_GetAppStatus_Passthrough(t *testing.T) {
	inner := &fakeSyncClientCB{}
	cb := NewCircuitBreakerSyncClient(inner, CircuitBreakerConfig{
		OpenThreshold:  1,
		CloseThreshold: 1,
		RetryInterval:  time.Minute,
	}, slog.Default())

	// GetAppStatus should always pass through even when inner doesn't implement it
	_, _, err := cb.GetAppStatus(context.Background(), "app-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPGCircuitBreaker_TerminateOperation_Passthrough(t *testing.T) {
	inner := &fakeSyncClientCB{}
	cb := NewCircuitBreakerSyncClient(inner, CircuitBreakerConfig{
		OpenThreshold:  1,
		CloseThreshold: 1,
		RetryInterval:  time.Minute,
	}, slog.Default())

	err := cb.TerminateOperation(context.Background(), "app-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPGCircuitBreaker_GetSyncRevision_Passthrough(t *testing.T) {
	inner := &fakeSyncClientCB{}
	cb := NewCircuitBreakerSyncClient(inner, CircuitBreakerConfig{
		OpenThreshold:  1,
		CloseThreshold: 1,
		RetryInterval:  time.Minute,
	}, slog.Default())

	rev, err := cb.GetSyncRevision(context.Background(), "app-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rev != "" {
		t.Errorf("expected empty revision, got %q", rev)
	}
}

// The circuit state gauge follows every transition: 0 closed, 1 open,
// 2 half-open. Dashboards and alerts read these exact values.
func TestPGCircuitBreaker_StateGaugeFollowsTransitions(t *testing.T) {
	inner := &fakeSyncClientCB{triggerErr: fmt.Errorf("down")}
	cb := NewCircuitBreakerSyncClient(inner, CircuitBreakerConfig{
		OpenThreshold:  1,
		CloseThreshold: 1,
		RetryInterval:  1 * time.Millisecond,
	}, slog.Default())

	_ = cb.TriggerSync(context.Background(), "app-1")
	if got := testutil.ToFloat64(metrics.ArgoCDCircuitState); got != 1 {
		t.Fatalf("after opening: gauge = %v, want 1", got)
	}
	time.Sleep(5 * time.Millisecond)
	inner.triggerErr = nil
	if err := cb.TriggerSync(context.Background(), "app-1"); err != nil {
		t.Fatalf("recovery probe: %v", err)
	}

	if got := testutil.ToFloat64(metrics.ArgoCDCircuitState); got != 0 {
		t.Errorf("after recovery: gauge = %v, want 0", got)
	}
}
