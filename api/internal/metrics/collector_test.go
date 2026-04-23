// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"context"
	"testing"
	"time"
)

// =============================================================================
// PoolStatsCollector tests (previously 0% coverage)
// =============================================================================

// We cannot use a real pgxpool.Pool without a database. However, we CAN test
// the NewPoolStatsCollector constructor and the Start/Stop lifecycle by
// using a nil pool and verifying the struct is created correctly.
// The collect() method would panic on nil pool, so we only test the
// lifecycle (Start/Stop) with a cancelled context to prevent the ticker
// from firing.

func TestNewPoolStatsCollector(t *testing.T) {
	c := NewPoolStatsCollector(nil, nil)
	if c == nil {
		t.Fatal("expected non-nil collector")
	}
	if c.stop == nil {
		t.Fatal("expected non-nil stop channel")
	}
}

func TestPoolStatsCollector_StartStop_DoesNotPanic(t *testing.T) {
	c := NewPoolStatsCollector(nil, nil)

	// Use a cancelled context so the goroutine exits immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c.Start(ctx, 1*time.Second)
	// Give goroutine time to start and exit
	time.Sleep(10 * time.Millisecond)
}

func TestPoolStatsCollector_Stop_ClosesChannel(t *testing.T) {
	c := NewPoolStatsCollector(nil, nil)

	// Start with a context that won't cancel
	ctx := context.Background()
	c.Start(ctx, 1*time.Hour) // long interval so ticker doesn't fire

	// Stop should close the channel and terminate the goroutine
	c.Stop()

	// Verify the stop channel is closed
	select {
	case <-c.stop:
		// expected: channel is closed
	default:
		t.Error("stop channel should be closed after Stop()")
	}
}
