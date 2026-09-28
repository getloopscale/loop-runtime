// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// poolStat is the part of a pgxpool.Stat snapshot the collector reads. The
// acquire counters and the acquire duration are CUMULATIVE since the pool was
// created.
type poolStat interface {
	AcquiredConns() int32
	IdleConns() int32
	TotalConns() int32
	MaxConns() int32
	AcquireCount() int64
	EmptyAcquireCount() int64
	AcquireDuration() time.Duration
}

// PoolStatsCollector periodically exports pgx connection pool stats: gauges
// for the current connection counts, and — because pgx reports its acquire
// counters as running totals — only the increase since the previous tick for
// the counters and the acquire-wait histogram.
type PoolStatsCollector struct {
	stat   func() poolStat
	logger *slog.Logger
	stop   chan struct{}

	prevAcquires        int64
	prevEmptyAcquires   int64
	prevAcquireDuration time.Duration
}

// NewPoolStatsCollector creates a collector that exports pool stats every interval.
func NewPoolStatsCollector(pool *pgxpool.Pool, logger *slog.Logger) *PoolStatsCollector {
	return newPoolStatsCollector(func() poolStat { return pool.Stat() }, logger)
}

func newPoolStatsCollector(stat func() poolStat, logger *slog.Logger) *PoolStatsCollector {
	return &PoolStatsCollector{
		stat:   stat,
		logger: logger,
		stop:   make(chan struct{}),
	}
}

// Start begins periodic collection. Call Stop() to terminate.
func (c *PoolStatsCollector) Start(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				c.collect()
			case <-c.stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop terminates the periodic collection.
func (c *PoolStatsCollector) Stop() {
	close(c.stop)
}

func (c *PoolStatsCollector) collect() {
	stats := c.stat()
	DBConnectionsActive.Set(float64(stats.AcquiredConns()))
	DBConnectionsIdle.Set(float64(stats.IdleConns()))
	DBConnectionsTotal.Set(float64(stats.TotalConns()))
	// P2-9: Additional pool metrics for saturation/contention visibility.
	DBPoolMaxConns.Set(float64(stats.MaxConns()))

	acquires := stats.AcquireCount() - c.prevAcquires
	waited := stats.AcquireDuration() - c.prevAcquireDuration
	emptyAcquires := stats.EmptyAcquireCount() - c.prevEmptyAcquires
	c.prevAcquires = stats.AcquireCount()
	c.prevAcquireDuration = stats.AcquireDuration()
	c.prevEmptyAcquires = stats.EmptyAcquireCount()

	if acquires > 0 {
		DBPoolAcquireCount.Add(float64(acquires))
		// pgx exposes only the total wait, so the histogram records the mean
		// wait per acquire over each tick — never a running total.
		DBPoolAcquireDuration.Observe(waited.Seconds() / float64(acquires))
	}
	if emptyAcquires > 0 {
		DBPoolEmptyAcquireCount.Add(float64(emptyAcquires))
	}
}
