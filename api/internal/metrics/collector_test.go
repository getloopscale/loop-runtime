// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
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

// fakeStat is a pool snapshot; pgxpool's acquire counters are cumulative
// since the pool was created.
type fakeStat struct {
	acquires, emptyAcquires int64
	acquireDuration         time.Duration
}

func (s fakeStat) AcquiredConns() int32           { return 1 }
func (s fakeStat) IdleConns() int32               { return 1 }
func (s fakeStat) TotalConns() int32              { return 2 }
func (s fakeStat) MaxConns() int32                { return 10 }
func (s fakeStat) AcquireCount() int64            { return s.acquires }
func (s fakeStat) EmptyAcquireCount() int64       { return s.emptyAcquires }
func (s fakeStat) AcquireDuration() time.Duration { return s.acquireDuration }

func TestPoolStatsCollectorAddsOnlyWhatHappenedSinceTheLastTick(t *testing.T) {
	// Arrange: two ticks; the pool's cumulative counters go 10 -> 25 and 2 -> 3.
	snapshots := []fakeStat{
		{acquires: 10, emptyAcquires: 2, acquireDuration: time.Second},
		{acquires: 25, emptyAcquires: 3, acquireDuration: 4 * time.Second},
	}
	i := 0
	c := newPoolStatsCollector(func() poolStat { s := snapshots[i]; i++; return s }, nil)
	before := testutil.ToFloat64(DBPoolAcquireCount)
	emptyBefore := testutil.ToFloat64(DBPoolEmptyAcquireCount)

	// Act
	c.collect()
	c.collect()

	// Assert: the counters advanced by the pool's totals (25, 3), not by the
	// running sum of totals (35, 5).
	if got := testutil.ToFloat64(DBPoolAcquireCount) - before; got != 25 {
		t.Errorf("acquire counter advanced by %v, want 25", got)
	}
	if got := testutil.ToFloat64(DBPoolEmptyAcquireCount) - emptyBefore; got != 3 {
		t.Errorf("empty-acquire counter advanced by %v, want 3", got)
	}
}

func TestPoolStatsCollectorObservesTheMeanAcquireWaitOfEachTick(t *testing.T) {
	snapshots := []fakeStat{
		{acquires: 10, acquireDuration: time.Second},
		{acquires: 20, acquireDuration: 3 * time.Second}, // 10 acquires waited 2s: 0.2s each
		{acquires: 20, acquireDuration: 3 * time.Second}, // idle tick: nothing to observe
	}
	i := 0
	c := newPoolStatsCollector(func() poolStat { s := snapshots[i]; i++; return s }, nil)
	sumBefore, countBefore := histogramSumCount(t)

	c.collect()
	c.collect()
	c.collect()

	sum, count := histogramSumCount(t)
	if count-countBefore != 2 {
		t.Fatalf("observations = %v, want 2 (first tick and second tick; idle tick skipped)", count-countBefore)
	}
	// first tick: 1s / 10 = 0.1s; second: 2s / 10 = 0.2s
	if got := sum - sumBefore; got < 0.299 || got > 0.301 {
		t.Errorf("observed sum = %v, want 0.3 (per-acquire means, not cumulative totals)", got)
	}
}

func histogramSumCount(t *testing.T) (float64, uint64) {
	t.Helper()
	m := &dto.Metric{}
	if err := DBPoolAcquireDuration.(prometheus.Metric).Write(m); err != nil {
		t.Fatal(err)
	}
	return m.GetHistogram().GetSampleSum(), m.GetHistogram().GetSampleCount()
}
