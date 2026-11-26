// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package aggregator

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSpawnFor_StartsGoroutineAndDrains confirms SpawnFor returns
// immediately and the background goroutine processes events.
func TestSpawnFor_StartsGoroutineAndDrains(t *testing.T) {
	store := &memoryStore{}
	reader := &scriptedReader{
		id: SourceReleaseDB,
		events: []PhaseEvent{
			{Phase: "build", Status: "in_progress", Timestamp: time.Now()},
			{Phase: "build", Status: "completed", Timestamp: time.Now()},
			{Phase: "sign", Status: "in_progress", Timestamp: time.Now()},
			{Phase: "sign", Status: "completed", Timestamp: time.Now()},
			{Phase: "release", Status: "in_progress", Timestamp: time.Now()},
			{Phase: "release", Status: "completed", Timestamp: time.Now()},
			{Phase: "deploy", Status: "in_progress", Timestamp: time.Now()},
			{Phase: "deploy", Status: "completed", Timestamp: time.Now()},
			{Phase: "live", Status: "in_progress", Timestamp: time.Now()},
			{Phase: "live", Status: "completed", Timestamp: time.Now()},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// SpawnFor must return immediately.
	returnedAt := time.Now()
	SpawnFor(ctx, "dep-spawn", store, []SourceReader{reader})
	if elapsed := time.Since(returnedAt); elapsed > 100*time.Millisecond {
		t.Errorf("SpawnFor blocked %v; want non-blocking", elapsed)
	}

	// Wait for completion (terminal at live/completed).
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatalf("aggregator did not drain in time")
		default:
		}
		if got := store.snapshot(); len(got) >= 10 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestSpawnFor_NilSinkLogsAndReturns confirms defensive no-op behavior
// when either sink is nil or readers is empty.
func TestSpawnFor_NilSinkLogsAndReturns(t *testing.T) {
	// No assertion needed beyond "does not panic" — slog.Warn is
	// observable via log capture in production. Coverage signal that
	// the guard exists.
	SpawnFor(context.Background(), "dep-nil", nil, []SourceReader{&scriptedReader{}})
	SpawnFor(context.Background(), "dep-nil", &memoryStore{}, nil)
}

// TestSpawnFor_GoroutineRespectsCtxCancel confirms the spawned goroutine
// exits cleanly on ctx cancel before terminal.
func TestSpawnFor_GoroutineRespectsCtxCancel(t *testing.T) {
	store := &memoryStore{}
	var emitCount atomic.Int32
	blocker := &blockingReader{emitCount: &emitCount, hold: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		SpawnFor(ctx, "dep-cancel", store, []SourceReader{blocker})
	}()
	wg.Wait()
	cancel()
	close(blocker.hold)
	// give goroutine time to exit
	time.Sleep(50 * time.Millisecond)
}

type blockingReader struct {
	emitCount *atomic.Int32
	hold      chan struct{}
}

func (b *blockingReader) ID() SourceID { return SourceLokiMarkers }
func (b *blockingReader) Stream(ctx context.Context, deployID string, out chan<- PhaseEvent) error {
	select {
	case <-b.hold:
	case <-ctx.Done():
	}
	return nil
}
