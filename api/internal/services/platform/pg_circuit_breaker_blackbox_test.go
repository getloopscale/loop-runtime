// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package platform_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/services/platform"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

// --- Fake inner SyncClient ---

type fakeInnerSync struct {
	triggerErr   error
	waitErr      error
	triggerCalls int
	waitCalls    int
}

func (f *fakeInnerSync) TriggerSync(_ context.Context, _ string) error {
	f.triggerCalls++
	return f.triggerErr
}

func (f *fakeInnerSync) WaitForSync(_ context.Context, _ string, _ time.Duration, _ []string) error {
	f.waitCalls++
	return f.waitErr
}

func (f *fakeInnerSync) GetAppStatus(_ context.Context, _ string, _ []string) (bool, bool, error) {
	return true, true, nil
}

func (f *fakeInnerSync) GetSyncRevision(_ context.Context, _ string) (string, error) {
	return "abc123", nil
}

// Sprint B T1.1 (ADR D1): stub methods to satisfy the widened SyncClient
// interface. The CB unit tests cover preCheck/recordResult around
// Trigger/Wait; ForceReconcile + TerminateOperation share the same
// preCheck contract and are exercised in internal/services tests.
func (f *fakeInnerSync) ForceReconcile(_ context.Context, _ string) error {
	return f.triggerErr
}
func (f *fakeInnerSync) TerminateOperation(_ context.Context, _ string) error {
	return f.triggerErr
}

// --- Helper ---

func defaultCBConfig() platform.CircuitBreakerConfig {
	return platform.CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 2,
		RetryInterval:  30 * time.Second,
	}
}

// --- Tests ---

func TestCircuitBreaker_ClosedState_TriggerSyncPassesThrough(t *testing.T) {
	// Arrange
	inner := &fakeInnerSync{}
	cb := platform.NewCircuitBreakerSyncClient(inner, defaultCBConfig(), testLogger)

	// Act
	err := cb.TriggerSync(context.Background(), "my-app")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if inner.triggerCalls != 1 {
		t.Errorf("expected inner TriggerSync to be called 1 time, got %d", inner.triggerCalls)
	}
}

func TestCircuitBreaker_ClosedState_WaitForSyncPassesThrough(t *testing.T) {
	// Arrange
	inner := &fakeInnerSync{}
	cb := platform.NewCircuitBreakerSyncClient(inner, defaultCBConfig(), testLogger)

	// Act
	err := cb.WaitForSync(context.Background(), "my-app", 30*time.Second, nil)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if inner.waitCalls != 1 {
		t.Errorf("expected inner WaitForSync to be called 1 time, got %d", inner.waitCalls)
	}
}

func TestCircuitBreaker_OpenState_RejectsTriggerSync(t *testing.T) {
	// Arrange: cause enough failures to open the circuit, then verify rejection
	inner := &fakeInnerSync{triggerErr: errors.New("connection refused")}
	cb := platform.NewCircuitBreakerSyncClient(inner, defaultCBConfig(), testLogger)

	// Open the circuit with 3 consecutive failures
	for i := 0; i < 3; i++ {
		_ = cb.TriggerSync(context.Background(), "my-app")
	}

	// Reset inner to track new calls
	inner.triggerCalls = 0
	inner.triggerErr = nil

	// Act: next call should be rejected (circuit is open, retry interval not elapsed)
	err := cb.TriggerSync(context.Background(), "my-app")

	// Assert
	if !errors.Is(err, loopzeroerrors.ErrPlatformDegraded) {
		t.Fatalf("expected ErrPlatformDegraded, got %v", err)
	}
	if inner.triggerCalls != 0 {
		t.Errorf("expected inner TriggerSync NOT to be called, got %d calls", inner.triggerCalls)
	}
}

func TestCircuitBreaker_OpenState_RejectsWaitForSync(t *testing.T) {
	// Arrange: open the circuit
	inner := &fakeInnerSync{triggerErr: errors.New("connection refused")}
	cb := platform.NewCircuitBreakerSyncClient(inner, defaultCBConfig(), testLogger)

	for i := 0; i < 3; i++ {
		_ = cb.TriggerSync(context.Background(), "my-app")
	}

	inner.waitCalls = 0

	// Act
	err := cb.WaitForSync(context.Background(), "my-app", 30*time.Second, nil)

	// Assert
	if !errors.Is(err, loopzeroerrors.ErrPlatformDegraded) {
		t.Fatalf("expected ErrPlatformDegraded, got %v", err)
	}
	if inner.waitCalls != 0 {
		t.Errorf("expected inner WaitForSync NOT to be called, got %d calls", inner.waitCalls)
	}
}

func TestCircuitBreaker_HalfOpen_AllowsRecoveryProbe(t *testing.T) {
	// Arrange: use a very short retry interval so half-open triggers immediately
	cfg := platform.CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 2,
		RetryInterval:  1 * time.Millisecond, // very short so test doesn't need to sleep
	}
	inner := &fakeInnerSync{triggerErr: errors.New("connection refused")}
	cb := platform.NewCircuitBreakerSyncClient(inner, cfg, testLogger)

	// Open the circuit
	for i := 0; i < 3; i++ {
		_ = cb.TriggerSync(context.Background(), "my-app")
	}

	// intentional: testing real-time behavior — wait past the 1ms retry interval
	time.Sleep(5 * time.Millisecond)

	// Reset inner: succeeds now
	inner.triggerCalls = 0
	inner.triggerErr = nil

	// Act
	err := cb.TriggerSync(context.Background(), "my-app")

	// Assert: request allowed through (recovery probe)
	if err != nil {
		t.Fatalf("expected no error (recovery probe), got %v", err)
	}
	if inner.triggerCalls != 1 {
		t.Errorf("expected inner TriggerSync to be called for recovery probe, got %d", inner.triggerCalls)
	}
}

func TestCircuitBreaker_ClosedToOpen_AfterThresholdFailures(t *testing.T) {
	// Arrange: start closed, cause 3 consecutive failures (open threshold)
	inner := &fakeInnerSync{triggerErr: errors.New("connection refused")}
	cb := platform.NewCircuitBreakerSyncClient(inner, defaultCBConfig(), testLogger)

	// Act: 3 failures
	for i := 0; i < 3; i++ {
		_ = cb.TriggerSync(context.Background(), "my-app")
	}

	// Assert: circuit should be open now (next call rejected)
	inner.triggerCalls = 0
	inner.triggerErr = nil
	err := cb.TriggerSync(context.Background(), "my-app")
	if !errors.Is(err, loopzeroerrors.ErrPlatformDegraded) {
		t.Fatalf("expected circuit to be open (ErrPlatformDegraded), got %v", err)
	}
	if inner.triggerCalls != 0 {
		t.Errorf("expected inner NOT to be called when circuit is open, got %d calls", inner.triggerCalls)
	}
}

func TestCircuitBreaker_OpenToClosed_AfterThresholdSuccesses(t *testing.T) {
	// Arrange: open the circuit, then allow recovery probes to close it
	cfg := platform.CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 2, // 2 successes to close
		RetryInterval:  1 * time.Millisecond,
	}
	inner := &fakeInnerSync{triggerErr: errors.New("connection refused")}
	cb := platform.NewCircuitBreakerSyncClient(inner, cfg, testLogger)

	// Open the circuit
	for i := 0; i < 3; i++ {
		_ = cb.TriggerSync(context.Background(), "my-app")
	}

	// Inner now succeeds
	inner.triggerErr = nil

	// Act: 2 successful recovery probes (with waits to pass retry interval)
	time.Sleep(5 * time.Millisecond) // intentional: testing real-time behavior — wait past 1ms retry interval
	_ = cb.TriggerSync(context.Background(), "my-app")
	time.Sleep(5 * time.Millisecond) // intentional: testing real-time behavior — wait past 1ms retry interval
	_ = cb.TriggerSync(context.Background(), "my-app")

	// Assert: circuit should be closed — next call succeeds without delay
	inner.triggerCalls = 0
	err := cb.TriggerSync(context.Background(), "my-app")
	if err != nil {
		t.Fatalf("expected circuit to be closed, got %v", err)
	}
	if inner.triggerCalls != 1 {
		t.Errorf("expected inner to be called (circuit closed), got %d calls", inner.triggerCalls)
	}
}

func TestCircuitBreaker_InnerErrorPropagated(t *testing.T) {
	// Arrange: circuit closed, inner returns error
	innerErr := errors.New("argocd 503")
	inner := &fakeInnerSync{triggerErr: innerErr}
	cb := platform.NewCircuitBreakerSyncClient(inner, defaultCBConfig(), testLogger)

	// Act
	err := cb.TriggerSync(context.Background(), "my-app")

	// Assert: inner error is returned to caller
	if err == nil {
		t.Fatal("expected inner error to be propagated")
	}
	if err.Error() != "argocd 503" {
		t.Errorf("expected 'argocd 503', got %q", err.Error())
	}
}

func TestCircuitBreaker_AuthFailure_DoesNotOpenCircuit(t *testing.T) {
	// 401 errors MUST NOT count toward circuit breaker threshold (MEET-013)
	inner := &fakeInnerSync{triggerErr: loopzeroerrors.ErrDeployServiceAuthFailed}
	cb := platform.NewCircuitBreakerSyncClient(inner, defaultCBConfig(), testLogger)

	// Send 10 auth failures — more than the open threshold (3)
	for i := 0; i < 10; i++ {
		err := cb.TriggerSync(context.Background(), "my-app")
		// Error should be propagated as-is (not ErrPlatformDegraded)
		if !errors.Is(err, loopzeroerrors.ErrDeployServiceAuthFailed) {
			t.Fatalf("iteration %d: expected ErrDeployServiceAuthFailed, got %v", i, err)
		}
	}

	// Circuit should still be CLOSED — auth failures don't count
	// Verify by sending a successful request (inner now succeeds)
	inner.triggerErr = nil
	inner.triggerCalls = 0
	err := cb.TriggerSync(context.Background(), "my-app")
	if err != nil {
		t.Fatalf("expected success after auth failures (circuit should be closed), got %v", err)
	}
	if inner.triggerCalls != 1 {
		t.Errorf("expected inner to be called (circuit closed), got %d calls", inner.triggerCalls)
	}
}

func TestCircuitBreaker_ServiceFailure_StillOpensCircuit(t *testing.T) {
	// 503 errors MUST still open the circuit (regression test)
	inner := &fakeInnerSync{triggerErr: errors.New("argocd: connection refused")}
	cb := platform.NewCircuitBreakerSyncClient(inner, defaultCBConfig(), testLogger)

	// Send 3 service failures (threshold)
	for i := 0; i < 3; i++ {
		_ = cb.TriggerSync(context.Background(), "my-app")
	}

	// Circuit should be OPEN — next call should return ErrPlatformDegraded
	err := cb.TriggerSync(context.Background(), "my-app")
	if !errors.Is(err, loopzeroerrors.ErrPlatformDegraded) {
		t.Fatalf("expected ErrPlatformDegraded (circuit open), got %v", err)
	}
}

func TestCircuitBreaker_GetAppStatus_DelegatesToInner(t *testing.T) {
	inner := &fakeInnerSync{}
	cb := platform.NewCircuitBreakerSyncClient(inner, defaultCBConfig(), testLogger)

	synced, healthy, err := cb.GetAppStatus(context.Background(), "my-app", nil)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !synced || !healthy {
		t.Error("expected synced=true, healthy=true from inner")
	}
}

func TestCircuitBreaker_GetSyncRevision_DelegatesToInner(t *testing.T) {
	inner := &fakeInnerSync{}
	cb := platform.NewCircuitBreakerSyncClient(inner, defaultCBConfig(), testLogger)

	rev, err := cb.GetSyncRevision(context.Background(), "my-app")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if rev != "abc123" {
		t.Errorf("expected revision 'abc123', got %q", rev)
	}
}

func TestCircuitBreaker_SyncOperationInProgress_DoesNotOpenCircuit(t *testing.T) {
	// 409 "operation in progress" MUST NOT count toward circuit breaker threshold (MEET-2026-03-29-019).
	// A single stuck ArgoCD Application should not trip the circuit for the entire platform.
	inner := &fakeInnerSync{triggerErr: loopzeroerrors.ErrSyncOperationInProgress}
	cb := platform.NewCircuitBreakerSyncClient(inner, defaultCBConfig(), testLogger)

	// Send 10 x 409 — well above the open threshold (3)
	for i := 0; i < 10; i++ {
		err := cb.TriggerSync(context.Background(), "my-app")
		if !errors.Is(err, loopzeroerrors.ErrSyncOperationInProgress) {
			t.Fatalf("iteration %d: expected ErrSyncOperationInProgress, got %v", i, err)
		}
	}

	// Circuit should still be CLOSED — 409 errors are excluded
	inner.triggerErr = nil
	inner.triggerCalls = 0
	err := cb.TriggerSync(context.Background(), "my-app")
	if err != nil {
		t.Fatalf("expected success after 409 errors (circuit should be closed), got %v", err)
	}
	if inner.triggerCalls != 1 {
		t.Errorf("expected inner to be called (circuit closed), got %d calls", inner.triggerCalls)
	}
}

func TestCircuitBreaker_ImplementsSyncClient(t *testing.T) {
	// Compile-time check
	var _ deploycontracts.SyncClient = (*platform.CircuitBreakerSyncClient)(nil)
}
