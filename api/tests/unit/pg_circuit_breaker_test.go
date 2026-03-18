package unit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/services"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// --- Fake CircuitBreakerStore (implements services.CircuitBreakerStore) ---

type fakeCircuitBreakerStore struct {
	state      *db.CircuitBreakerState
	getErr     error
	failErr    error
	successErr error
}

func newClosedCircuitBreakerStore() *fakeCircuitBreakerStore {
	return &fakeCircuitBreakerStore{
		state: &db.CircuitBreakerState{
			Key:   "argocd",
			State: "closed",
		},
	}
}

func newOpenCircuitBreakerStore(openedAt time.Time) *fakeCircuitBreakerStore {
	return &fakeCircuitBreakerStore{
		state: &db.CircuitBreakerState{
			Key:          "argocd",
			State:        "open",
			FailureCount: 3,
			OpenedAt:     &openedAt,
		},
	}
}

func (f *fakeCircuitBreakerStore) Get(_ context.Context, _ string) (*db.CircuitBreakerState, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	c := *f.state
	return &c, nil
}

func (f *fakeCircuitBreakerStore) RecordFailure(_ context.Context, _ string, openThreshold int) (*db.CircuitBreakerState, error) {
	if f.failErr != nil {
		return nil, f.failErr
	}
	f.state.FailureCount++
	f.state.SuccessCount = 0
	now := time.Now()
	f.state.LastFailure = &now
	if f.state.FailureCount >= openThreshold {
		f.state.State = "open"
		f.state.OpenedAt = &now
	}
	c := *f.state
	return &c, nil
}

func (f *fakeCircuitBreakerStore) RecordSuccess(_ context.Context, _ string, closeThreshold int) (*db.CircuitBreakerState, error) {
	if f.successErr != nil {
		return nil, f.successErr
	}
	f.state.SuccessCount++
	f.state.FailureCount = 0
	now := time.Now()
	f.state.LastSuccess = &now
	if f.state.State == "open" && f.state.SuccessCount >= closeThreshold {
		f.state.State = "closed"
		f.state.OpenedAt = nil
	}
	c := *f.state
	return &c, nil
}

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

func (f *fakeInnerSync) WaitForSync(_ context.Context, _ string, _ time.Duration) error {
	f.waitCalls++
	return f.waitErr
}

// --- Helper ---

func defaultCBConfig() services.PGCircuitBreakerConfig {
	return services.PGCircuitBreakerConfig{
		Key:            "argocd",
		OpenThreshold:  3,
		CloseThreshold: 2,
		RetryInterval:  30 * time.Second,
	}
}

// --- Tests ---

func TestCircuitBreaker_ClosedState_TriggerSyncPassesThrough(t *testing.T) {
	// Arrange
	inner := &fakeInnerSync{}
	store := newClosedCircuitBreakerStore()
	cb := services.NewPGCircuitBreakerSyncClient(inner, store, defaultCBConfig())

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
	store := newClosedCircuitBreakerStore()
	cb := services.NewPGCircuitBreakerSyncClient(inner, store, defaultCBConfig())

	// Act
	err := cb.WaitForSync(context.Background(), "my-app", 30*time.Second)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if inner.waitCalls != 1 {
		t.Errorf("expected inner WaitForSync to be called 1 time, got %d", inner.waitCalls)
	}
}

func TestCircuitBreaker_OpenState_RejectsTriggerSync(t *testing.T) {
	// Arrange: circuit opened recently (within retry interval)
	openedAt := time.Now() // just opened — within 30s retry interval
	store := newOpenCircuitBreakerStore(openedAt)
	inner := &fakeInnerSync{}
	cb := services.NewPGCircuitBreakerSyncClient(inner, store, defaultCBConfig())

	// Act
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
	// Arrange
	openedAt := time.Now()
	store := newOpenCircuitBreakerStore(openedAt)
	inner := &fakeInnerSync{}
	cb := services.NewPGCircuitBreakerSyncClient(inner, store, defaultCBConfig())

	// Act
	err := cb.WaitForSync(context.Background(), "my-app", 30*time.Second)

	// Assert
	if !errors.Is(err, loopzeroerrors.ErrPlatformDegraded) {
		t.Fatalf("expected ErrPlatformDegraded, got %v", err)
	}
	if inner.waitCalls != 0 {
		t.Errorf("expected inner WaitForSync NOT to be called, got %d calls", inner.waitCalls)
	}
}

func TestCircuitBreaker_HalfOpen_AllowsRecoveryProbe(t *testing.T) {
	// Arrange: circuit opened long ago (past retry interval) → half-open
	openedAt := time.Now().Add(-60 * time.Second) // 60s ago, retry interval is 30s
	store := newOpenCircuitBreakerStore(openedAt)
	inner := &fakeInnerSync{} // inner succeeds
	cb := services.NewPGCircuitBreakerSyncClient(inner, store, defaultCBConfig())

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

func TestCircuitBreaker_HalfOpen_SuccessRecordsSuccess(t *testing.T) {
	// Arrange
	openedAt := time.Now().Add(-60 * time.Second)
	store := newOpenCircuitBreakerStore(openedAt)
	inner := &fakeInnerSync{} // succeeds
	cb := services.NewPGCircuitBreakerSyncClient(inner, store, defaultCBConfig())

	// Act
	_ = cb.TriggerSync(context.Background(), "my-app")

	// Assert: success was recorded (success_count incremented)
	if store.state.SuccessCount != 1 {
		t.Errorf("expected success count 1, got %d", store.state.SuccessCount)
	}
}

func TestCircuitBreaker_HalfOpen_FailureRecordsFailure(t *testing.T) {
	// Arrange
	openedAt := time.Now().Add(-60 * time.Second)
	store := newOpenCircuitBreakerStore(openedAt)
	inner := &fakeInnerSync{triggerErr: errors.New("argocd unreachable")}
	cb := services.NewPGCircuitBreakerSyncClient(inner, store, defaultCBConfig())

	// Act
	err := cb.TriggerSync(context.Background(), "my-app")

	// Assert
	if err == nil {
		t.Fatal("expected error from inner sync client")
	}
	// Failure recorded — failure count should increase
	if store.state.FailureCount != 4 { // was 3, now 4
		t.Errorf("expected failure count 4, got %d", store.state.FailureCount)
	}
}

func TestCircuitBreaker_ClosedToOpen_AfterThresholdFailures(t *testing.T) {
	// Arrange: start closed, cause 3 consecutive failures (open threshold)
	store := newClosedCircuitBreakerStore()
	inner := &fakeInnerSync{triggerErr: errors.New("connection refused")}
	cb := services.NewPGCircuitBreakerSyncClient(inner, store, defaultCBConfig())

	// Act: 3 failures
	for i := 0; i < 3; i++ {
		_ = cb.TriggerSync(context.Background(), "my-app")
	}

	// Assert: circuit should be open now
	if store.state.State != "open" {
		t.Errorf("expected circuit state 'open' after 3 failures, got %q", store.state.State)
	}
	if store.state.OpenedAt == nil {
		t.Error("expected OpenedAt to be set when circuit opens")
	}
}

func TestCircuitBreaker_OpenToClosed_AfterThresholdSuccesses(t *testing.T) {
	// Arrange: circuit is open but past retry interval (half-open)
	openedAt := time.Now().Add(-60 * time.Second)
	store := newOpenCircuitBreakerStore(openedAt)
	inner := &fakeInnerSync{} // succeeds
	cfg := services.PGCircuitBreakerConfig{
		Key:            "argocd",
		OpenThreshold:  3,
		CloseThreshold: 2, // 2 successes to close
		RetryInterval:  30 * time.Second,
	}
	cb := services.NewPGCircuitBreakerSyncClient(inner, store, cfg)

	// Act: 2 successful recovery probes
	_ = cb.TriggerSync(context.Background(), "my-app")
	_ = cb.TriggerSync(context.Background(), "my-app")

	// Assert: circuit should be closed after 2 successes
	if store.state.State != "closed" {
		t.Errorf("expected circuit state 'closed' after 2 successes, got %q", store.state.State)
	}
	if store.state.OpenedAt != nil {
		t.Error("expected OpenedAt to be nil after circuit closes")
	}
}

func TestCircuitBreaker_FailOpen_WhenStoreUnreachable(t *testing.T) {
	// Arrange: Get() returns an error (PG unreachable)
	store := newClosedCircuitBreakerStore()
	store.getErr = errors.New("connection refused to PG")
	inner := &fakeInnerSync{}
	cb := services.NewPGCircuitBreakerSyncClient(inner, store, defaultCBConfig())

	// Act
	err := cb.TriggerSync(context.Background(), "my-app")

	// Assert: fail-open — request should pass through
	if err != nil {
		t.Fatalf("expected nil error (fail-open), got %v", err)
	}
	if inner.triggerCalls != 1 {
		t.Errorf("expected inner to be called (fail-open), got %d calls", inner.triggerCalls)
	}
}

func TestCircuitBreaker_InnerErrorPropagated(t *testing.T) {
	// Arrange: circuit closed, inner returns error
	store := newClosedCircuitBreakerStore()
	innerErr := errors.New("argocd 503")
	inner := &fakeInnerSync{triggerErr: innerErr}
	cb := services.NewPGCircuitBreakerSyncClient(inner, store, defaultCBConfig())

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

func TestCircuitBreaker_ImplementsSyncClient(t *testing.T) {
	// Compile-time check
	var _ services.SyncClient = (*services.PGCircuitBreakerSyncClient)(nil)
}
