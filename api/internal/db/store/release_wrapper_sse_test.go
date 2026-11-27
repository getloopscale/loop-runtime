// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// T1.1 of plan 2026-06-15-j18-t1.3.b-sse-pg-notify-backend v1.1.
//
// Hermetic tests for the SSE emit wiring added to AppendSideEffect.
// The deep PG-touching AppendSideEffect behavior is covered by integration
// tests (release_store_column_cascade_test, concurrent_builds_test,
// rollback_test). These hermetic tests focus on the contract surface
// added by T1.1: constructor panics on nil eventStore (EC-1 absorption)
// and NoopEventStore conforms to sseEventStore + behaves as no-op.

// fakeEventStore captures Append calls for assertion.
type fakeEventStore struct {
	calls []eventStoreCall
	err   error
}

type eventStoreCall struct {
	BuildID   string
	EventType string
	Data      json.RawMessage
}

func (f *fakeEventStore) Append(_ context.Context, buildID string, eventType string, data json.RawMessage) (int64, error) {
	f.calls = append(f.calls, eventStoreCall{
		BuildID:   buildID,
		EventType: eventType,
		Data:      data,
	})
	if f.err != nil {
		return 0, f.err
	}
	return int64(len(f.calls)), nil
}

// TestNewReleaseStoreSqlc_PanicsOnNilEventStore — EC-1 absorption: constructor
// MUST panic on nil eventStore to surface mis-wired startups loud + fast.
// Prevents silent dead-feature where SSE realtime path appears installed
// but emits zero events forever.
func TestNewReleaseStoreSqlc_PanicsOnNilEventStore(t *testing.T) {
	t.Parallel()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on nil eventStore, got no panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected panic with string message, got %T: %v", r, r)
		}
		// Defense: message must mention EventStore so operator immediately
		// understands the canonical wiring fix (don't just say "nil").
		if !contains(msg, "EventStore") {
			t.Errorf("panic message must mention EventStore for operator clarity, got: %q", msg)
		}
	}()

	_ = NewReleaseStoreSqlc(nil, nil)
}

// TestNewReleaseStoreSqlc_AcceptsNoopEventStore — NoopEventStore satisfies
// the sseEventStore contract (canonical read-only sentinel for
// ReleaseInspector + other read-only paths per setup_server.go:276).
func TestNewReleaseStoreSqlc_AcceptsNoopEventStore(t *testing.T) {
	t.Parallel()

	// Must NOT panic — NoopEventStore is non-nil and satisfies the interface.
	store := NewReleaseStoreSqlc(nil, NoopEventStore{})
	if store == nil {
		t.Fatal("expected non-nil store with NoopEventStore, got nil")
	}
	if store.eventStore == nil {
		t.Fatal("expected store.eventStore wired, got nil")
	}
}

// TestNoopEventStore_AppendReturnsZeroNil — NoopEventStore conforms to the
// sseEventStore contract by accepting Append calls + returning (0, nil).
// Read-only callers that wire NoopEventStore by design never reach
// AppendSideEffect; if they did, the event is silently dropped (correct
// behavior for the no-op contract).
func TestNoopEventStore_AppendReturnsZeroNil(t *testing.T) {
	t.Parallel()

	noop := NoopEventStore{}
	id, err := noop.Append(context.Background(), "release-123", "signal.changed", json.RawMessage(`{"status":"Healthy"}`))
	if id != 0 {
		t.Errorf("expected id=0 from no-op, got %d", id)
	}
	if err != nil {
		t.Errorf("expected nil err from no-op, got %v", err)
	}
}

// TestNewReleaseStoreSqlc_AcceptsFakeEventStoreImplementation — verifies
// the sseEventStore interface is structurally compatible with a custom
// test fake (defense for downstream test authors).
func TestNewReleaseStoreSqlc_AcceptsFakeEventStoreImplementation(t *testing.T) {
	t.Parallel()

	fake := &fakeEventStore{}
	store := NewReleaseStoreSqlc(nil, fake)
	if store == nil {
		t.Fatal("expected non-nil store with fake eventStore")
	}
}

// TestFakeEventStore_AppendCapturesCall — verifies the fake helper itself
// captures calls correctly (meta-test for downstream consumer correctness).
func TestFakeEventStore_AppendCapturesCall(t *testing.T) {
	t.Parallel()

	fake := &fakeEventStore{}
	_, _ = fake.Append(context.Background(), "rel-1", "signal.changed", json.RawMessage(`{"x":1}`))

	if len(fake.calls) != 1 {
		t.Fatalf("expected 1 captured call, got %d", len(fake.calls))
	}
	if fake.calls[0].BuildID != "rel-1" {
		t.Errorf("BuildID: got %q, want rel-1", fake.calls[0].BuildID)
	}
	if fake.calls[0].EventType != "signal.changed" {
		t.Errorf("EventType: got %q, want signal.changed", fake.calls[0].EventType)
	}
}

// TestFakeEventStore_AppendReturnsErr — verifies error injection path of
// the fake (used by deeper tests that inject EventStore.Append failure).
func TestFakeEventStore_AppendReturnsErr(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("simulated failure")
	fake := &fakeEventStore{err: expectedErr}
	_, err := fake.Append(context.Background(), "rel-1", "signal.changed", json.RawMessage(`{}`))

	if err == nil {
		t.Fatal("expected error from fake, got nil")
	}
	if err.Error() != expectedErr.Error() {
		t.Errorf("err: got %q, want %q", err.Error(), expectedErr.Error())
	}
}

// contains is a tiny string-contains helper to avoid importing strings.
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
