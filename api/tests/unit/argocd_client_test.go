package unit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/services"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// --- Fake SyncClient for circuit breaker tests ---

type fakeCBSyncClient struct {
	triggerErr error
	waitErr    error
	callCount  int
}

func (f *fakeCBSyncClient) TriggerSync(ctx context.Context, appName string) error {
	f.callCount++
	return f.triggerErr
}

func (f *fakeCBSyncClient) WaitForSync(ctx context.Context, appName string, timeout time.Duration) error {
	f.callCount++
	return f.waitErr
}

// --- Tests ---

func TestCircuitBreaker_SyncSuccess_CircuitClosed(t *testing.T) {
	// Arrange
	inner := &fakeCBSyncClient{}
	cb := services.NewCircuitBreakerSyncClient(inner, services.CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 2,
		RetryInterval:  30 * time.Second,
	})

	// Act
	err := cb.TriggerSync(context.Background(), "my-app")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if inner.callCount != 1 {
		t.Fatalf("expected inner to be called 1 time, got %d", inner.callCount)
	}
	if cb.State() != services.CircuitClosed {
		t.Fatalf("expected circuit CLOSED, got %v", cb.State())
	}
}

func TestCircuitBreaker_FailureUnderThreshold_StillClosed(t *testing.T) {
	// Arrange
	inner := &fakeCBSyncClient{triggerErr: errors.New("argocd unavailable")}
	cb := services.NewCircuitBreakerSyncClient(inner, services.CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 2,
		RetryInterval:  30 * time.Second,
	})

	// Act — 2 failures (under threshold of 3)
	_ = cb.TriggerSync(context.Background(), "my-app")
	_ = cb.TriggerSync(context.Background(), "my-app")

	// Assert — circuit still closed
	if cb.State() != services.CircuitClosed {
		t.Fatalf("expected circuit CLOSED after 2 failures, got %v", cb.State())
	}
	if inner.callCount != 2 {
		t.Fatalf("expected inner called 2 times, got %d", inner.callCount)
	}
}

func TestCircuitBreaker_ThreeFailures_CircuitOpens(t *testing.T) {
	// Arrange
	inner := &fakeCBSyncClient{triggerErr: errors.New("argocd unavailable")}
	cb := services.NewCircuitBreakerSyncClient(inner, services.CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 2,
		RetryInterval:  30 * time.Second,
	})

	// Act — 3 failures
	_ = cb.TriggerSync(context.Background(), "my-app")
	_ = cb.TriggerSync(context.Background(), "my-app")
	_ = cb.TriggerSync(context.Background(), "my-app")

	// Assert — circuit is now OPEN
	if cb.State() != services.CircuitOpen {
		t.Fatalf("expected circuit OPEN after 3 failures, got %v", cb.State())
	}

	// Next call should be rejected without calling inner
	callsBefore := inner.callCount
	err := cb.TriggerSync(context.Background(), "my-app")
	if err == nil {
		t.Fatal("expected error when circuit is OPEN")
	}
	if !errors.Is(err, loopzeroerrors.ErrPlatformDegraded) {
		t.Fatalf("expected ErrPlatformDegraded, got %v", err)
	}
	if inner.callCount != callsBefore {
		t.Fatal("inner should NOT be called when circuit is OPEN")
	}
}

func TestCircuitBreaker_OpenRejectsCalls(t *testing.T) {
	// Arrange — force circuit open
	inner := &fakeCBSyncClient{triggerErr: errors.New("argocd unavailable")}
	cb := services.NewCircuitBreakerSyncClient(inner, services.CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 2,
		RetryInterval:  1 * time.Hour, // long interval so no recovery probe
	})

	// Trip the circuit
	for i := 0; i < 3; i++ {
		_ = cb.TriggerSync(context.Background(), "my-app")
	}

	// Act
	inner.callCount = 0
	err := cb.TriggerSync(context.Background(), "my-app")

	// Assert
	if err == nil {
		t.Fatal("expected error from open circuit")
	}
	if !errors.Is(err, loopzeroerrors.ErrPlatformDegraded) {
		t.Fatalf("expected ErrPlatformDegraded, got %v", err)
	}
	if inner.callCount != 0 {
		t.Fatal("inner should not be called when circuit is OPEN")
	}

	// WaitForSync should also be rejected
	err = cb.WaitForSync(context.Background(), "my-app", 30*time.Second)
	if !errors.Is(err, loopzeroerrors.ErrPlatformDegraded) {
		t.Fatalf("WaitForSync: expected ErrPlatformDegraded, got %v", err)
	}
}

func TestCircuitBreaker_OpenRecovery_AllowsProbeAfterInterval(t *testing.T) {
	// Arrange — circuit open with very short retry interval
	inner := &fakeCBSyncClient{triggerErr: errors.New("argocd unavailable")}
	cb := services.NewCircuitBreakerSyncClient(inner, services.CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 2,
		RetryInterval:  1 * time.Millisecond, // very short for test
	})

	// Trip the circuit
	for i := 0; i < 3; i++ {
		_ = cb.TriggerSync(context.Background(), "my-app")
	}

	// Wait past retry interval
	time.Sleep(5 * time.Millisecond)

	// Act — inner now succeeds
	inner.triggerErr = nil
	inner.callCount = 0
	err := cb.TriggerSync(context.Background(), "my-app")

	// Assert — call was allowed through (recovery probe)
	if err != nil {
		t.Fatalf("expected no error during recovery probe, got %v", err)
	}
	if inner.callCount != 1 {
		t.Fatalf("expected inner called 1 time during recovery, got %d", inner.callCount)
	}
}

func TestCircuitBreaker_TwoSuccessesDuringRecovery_CircuitCloses(t *testing.T) {
	// Arrange
	inner := &fakeCBSyncClient{triggerErr: errors.New("argocd unavailable")}
	cb := services.NewCircuitBreakerSyncClient(inner, services.CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 2,
		RetryInterval:  1 * time.Millisecond,
	})

	// Trip the circuit
	for i := 0; i < 3; i++ {
		_ = cb.TriggerSync(context.Background(), "my-app")
	}
	if cb.State() != services.CircuitOpen {
		t.Fatal("circuit should be OPEN")
	}

	// Recovery: 2 successes
	inner.triggerErr = nil
	time.Sleep(5 * time.Millisecond)
	err1 := cb.TriggerSync(context.Background(), "my-app") // success 1
	if err1 != nil {
		t.Fatalf("recovery call 1 failed: %v", err1)
	}

	time.Sleep(5 * time.Millisecond)
	err2 := cb.TriggerSync(context.Background(), "my-app") // success 2
	if err2 != nil {
		t.Fatalf("recovery call 2 failed: %v", err2)
	}

	// Assert — circuit should be CLOSED again
	if cb.State() != services.CircuitClosed {
		t.Fatalf("expected circuit CLOSED after 2 recovery successes, got %v", cb.State())
	}
}

func TestCircuitBreaker_FailureDuringRecovery_StaysOpen(t *testing.T) {
	// Arrange
	inner := &fakeCBSyncClient{triggerErr: errors.New("argocd unavailable")}
	cb := services.NewCircuitBreakerSyncClient(inner, services.CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 2,
		RetryInterval:  1 * time.Millisecond,
	})

	// Trip the circuit
	for i := 0; i < 3; i++ {
		_ = cb.TriggerSync(context.Background(), "my-app")
	}

	// Recovery attempt 1: success
	inner.triggerErr = nil
	time.Sleep(5 * time.Millisecond)
	_ = cb.TriggerSync(context.Background(), "my-app")

	// Recovery attempt 2: failure
	inner.triggerErr = errors.New("still broken")
	time.Sleep(5 * time.Millisecond)
	_ = cb.TriggerSync(context.Background(), "my-app")

	// Assert — circuit stays OPEN, success count reset
	if cb.State() != services.CircuitOpen {
		t.Fatalf("expected circuit OPEN after failure during recovery, got %v", cb.State())
	}
}

func TestCircuitBreaker_State_ReturnsCorrectValue(t *testing.T) {
	// Arrange
	inner := &fakeCBSyncClient{}
	cb := services.NewCircuitBreakerSyncClient(inner, services.CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 2,
		RetryInterval:  1 * time.Millisecond,
	})

	// Assert — starts CLOSED
	if cb.State() != services.CircuitClosed {
		t.Fatalf("expected initial state CLOSED, got %v", cb.State())
	}

	// Trip the circuit
	inner.triggerErr = errors.New("fail")
	for i := 0; i < 3; i++ {
		_ = cb.TriggerSync(context.Background(), "my-app")
	}

	// Assert — now OPEN
	if cb.State() != services.CircuitOpen {
		t.Fatalf("expected state OPEN, got %v", cb.State())
	}

	// Recover
	inner.triggerErr = nil
	time.Sleep(5 * time.Millisecond)
	_ = cb.TriggerSync(context.Background(), "my-app")
	time.Sleep(5 * time.Millisecond)
	_ = cb.TriggerSync(context.Background(), "my-app")

	// Assert — back to CLOSED
	if cb.State() != services.CircuitClosed {
		t.Fatalf("expected state CLOSED after recovery, got %v", cb.State())
	}
}

func TestCircuitBreaker_WaitForSync_AlsoProtected(t *testing.T) {
	// Arrange — verify WaitForSync also goes through circuit breaker
	inner := &fakeCBSyncClient{waitErr: errors.New("sync timeout")}
	cb := services.NewCircuitBreakerSyncClient(inner, services.CircuitBreakerConfig{
		OpenThreshold:  3,
		CloseThreshold: 2,
		RetryInterval:  1 * time.Hour,
	})

	// Trip via WaitForSync
	for i := 0; i < 3; i++ {
		_ = cb.WaitForSync(context.Background(), "my-app", 30*time.Second)
	}

	// Assert — circuit opens from WaitForSync failures too
	if cb.State() != services.CircuitOpen {
		t.Fatalf("expected circuit OPEN after 3 WaitForSync failures, got %v", cb.State())
	}

	// Next call rejected
	err := cb.WaitForSync(context.Background(), "my-app", 30*time.Second)
	if !errors.Is(err, loopzeroerrors.ErrPlatformDegraded) {
		t.Fatalf("expected ErrPlatformDegraded, got %v", err)
	}
}
