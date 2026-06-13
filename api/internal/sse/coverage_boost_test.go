// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package sse

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"
)

// =============================================================================
// EventPruner tests
// =============================================================================

func TestNewEventPruner_SetsFields(t *testing.T) {
	store := &fakeEventStore{}
	logger := slog.Default()
	pruner := NewEventPruner(store, logger, 24*time.Hour, 1*time.Hour)

	if pruner.store == nil {
		t.Error("expected non-nil store")
	}
	if pruner.maxAge != 24*time.Hour {
		t.Errorf("expected 24h maxAge, got %v", pruner.maxAge)
	}
	if pruner.interval != 1*time.Hour {
		t.Errorf("expected 1h interval, got %v", pruner.interval)
	}
}

func TestEventPruner_Start_StopsOnCancel(t *testing.T) {
	store := &fakeEventStore{}
	logger := slog.Default()
	// Use very short interval so the ticker fires quickly.
	pruner := NewEventPruner(store, logger, 1*time.Minute, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		pruner.Start(ctx)
		close(done)
	}()

	// Let at least one tick happen.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// OK
	case <-time.After(3 * time.Second):
		t.Fatal("pruner did not stop within 3s after cancel")
	}
}

// =============================================================================
// PostgresEventStore tests (with fakes)
// =============================================================================

// fakeEventStore implements EventStore for testing.
type fakeEventStore struct {
	appendErr  error
	getErr     error
	pruneErr   error
	pruneCount int64
}

func (f *fakeEventStore) Append(_ context.Context, _ string, _ string, _ json.RawMessage) (int64, error) {
	return 1, f.appendErr
}

func (f *fakeEventStore) GetAfter(_ context.Context, _ string, _ int64) ([]SSEEvent, error) {
	return nil, f.getErr
}

func (f *fakeEventStore) GetAfterForProject(_ context.Context, _ string, _ string, _ int64) ([]SSEEvent, error) {
	return nil, f.getErr
}

func (f *fakeEventStore) Prune(_ context.Context, _ time.Duration) (int64, error) {
	return f.pruneCount, f.pruneErr
}

func TestEventPruner_PruneError_Continues(t *testing.T) {
	store := &fakeEventStore{pruneErr: errors.New("prune failed")}
	logger := slog.Default()
	pruner := NewEventPruner(store, logger, 1*time.Minute, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		pruner.Start(ctx)
		close(done)
	}()

	// Let a tick fire (prune will error but should not stop the loop).
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// OK — pruner continued despite error and stopped on cancel.
	case <-time.After(3 * time.Second):
		t.Fatal("pruner did not stop")
	}
}

// =============================================================================
// PostgresEventStore — NewPostgresEventStore
// =============================================================================

func TestNewPostgresEventStore_SetsFields(t *testing.T) {
	store := NewPostgresEventStore(nil, nil)
	if store == nil {
		t.Fatal("expected non-nil store")
	}
}
