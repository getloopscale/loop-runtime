// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package policy

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// --- Test doubles ---

type fakeIdempotencyCleanupStore struct {
	cleanedUp int64
	err       error
}

func (f *fakeIdempotencyCleanupStore) Cleanup(_ context.Context) (int64, error) {
	return f.cleanedUp, f.err
}

// --- Tests ---

func TestNewIdempotencyCleaner(t *testing.T) {
	store := &fakeIdempotencyCleanupStore{}
	cleaner := NewIdempotencyCleaner(store, slog.Default(), 5*time.Minute)
	if cleaner == nil {
		t.Fatal("expected non-nil cleaner")
	}
	if cleaner.interval != 5*time.Minute {
		t.Errorf("expected interval 5m, got %v", cleaner.interval)
	}
	if cleaner.store != store {
		t.Error("expected store to be set")
	}
}

func TestIdempotencyCleaner_Start_StopsOnContextCancel(t *testing.T) {
	store := &fakeIdempotencyCleanupStore{cleanedUp: 0}
	cleaner := NewIdempotencyCleaner(store, slog.Default(), 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		cleaner.Start(ctx)
		close(done)
	}()

	// Let it run briefly then cancel
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// OK - stopped cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("cleaner did not stop after context cancellation")
	}
}
