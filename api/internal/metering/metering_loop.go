// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metering

// metering_loop.go — the leader-gated driver of worker.RunOnce (plan
// metering-compute-loop T3.2, slice S4). The loop is started only on the leader
// (the caller's responsibility); each tick is gated by the kill-switch (EC-5),
// guarded by single-flight (no overlapping pass), and wrapped in a panic-recovery
// boundary (ADR-1 — a metering panic must never crash the controller).

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// runner is the narrow surface the loop drives (satisfied by *Worker). ISP: the
// loop needs only RunOnce, not the whole worker.
type runner interface {
	RunOnce(ctx context.Context) error
}

// killSwitch is the narrow kill-switch read (satisfied by *store.MeteringStore).
type killSwitch interface {
	IsMeteringEnabled(ctx context.Context) (enabled bool, found bool, err error)
}

// Loop drives one metering pass per tick until its context is cancelled.
type Loop struct {
	runner      runner
	killSwitch  killSwitch
	metrics     Metrics
	clock       Clock
	interval    time.Duration
	running     atomic.Bool // single-flight: true while a pass is in flight
	lastSuccess time.Time   // clock time of the last completed pass (freshness anchor)
}

// NewLoop wires the loop's collaborators + tick interval. lastSuccess is seeded to
// the construction time so the freshness gauge measures "seconds since loop start"
// until the first pass completes — a loop that starts but never completes a pass
// within ~3×interval alarms just as a loop that goes stale later does.
func NewLoop(r runner, ks killSwitch, m Metrics, clock Clock, interval time.Duration) *Loop {
	return &Loop{
		runner:      r,
		killSwitch:  ks,
		metrics:     m,
		clock:       clock,
		interval:    interval,
		lastSuccess: clock.Now(),
	}
}

// tick runs one metering pass, gated by the kill-switch + single-flight, inside a
// panic-recovery boundary. It NEVER returns an error and NEVER propagates a panic:
// a pass failure is observed via metrics + logs, and the controller keeps running
// (ADR-1). The order is single-flight → recover → freshness → kill-switch → timed
// RunOnce. lastSuccess is only ever written by the goroutine holding the
// single-flight (the CAS makes the body mutually exclusive), so no lock is needed.
func (l *Loop) tick(ctx context.Context) {
	// Single-flight: a slow pass must not overlap the next tick.
	if !l.running.CompareAndSwap(false, true) {
		return
	}
	// Registered BEFORE the recover defer so the lock is released even on a panic
	// (defers run LIFO: recover first, then this release).
	defer l.running.Store(false)

	defer func() {
		if r := recover(); r != nil {
			l.metrics.Panic()
			slog.ErrorContext(ctx, "metering: pass panicked (recovered — controller survives)", "panic", r)
		}
	}()

	// Freshness, measured at the START of the tick: seconds since the last completed
	// pass. A healthy loop hovers at ~interval; a loop whose passes stop completing
	// (repeated panic — lastSuccess frozen) grows this unboundedly → alert >3×interval.
	l.metrics.SecondsSinceLastSuccess(l.clock.Now().Sub(l.lastSuccess).Seconds())

	enabled, found, err := l.killSwitch.IsMeteringEnabled(ctx)
	if err != nil {
		// EC-5 fail-safe: an unreadable kill-switch HOLDS — never bill blind. Idle,
		// not stale: bump lastSuccess so the freshness gauge does not alarm.
		slog.WarnContext(ctx, "metering: kill-switch unreadable — skipping pass (fail-safe)", "err", err)
		l.lastSuccess = l.clock.Now()
		return
	}
	if !found {
		// EC-5: the config row is absent → DISABLED + WARN (operational alarm — a
		// seeded row was deleted). Idle, not stale.
		slog.WarnContext(ctx, "metering: kill-switch row absent — metering disabled (EC-5)")
		l.lastSuccess = l.clock.Now()
		return
	}
	if !enabled {
		// Normal operator-off state — observe-only ships disabled. Idle, not stale:
		// keep-alive so a deliberately-off loop never trips the freshness alert.
		l.lastSuccess = l.clock.Now()
		return
	}

	start := l.clock.Now()
	runErr := l.runner.RunOnce(ctx)
	// The pass ran to completion (even a degraded pass with per-tenant HOLDs means
	// the LOOP is alive) — record its wall-clock + anchor freshness. A panic returns
	// above without reaching here, leaving lastSuccess frozen (the alarm signal).
	l.metrics.PassDuration(l.clock.Now().Sub(start).Seconds())
	l.lastSuccess = l.clock.Now()
	if runErr != nil {
		// A degraded pass (one or more tenants failed) is logged, never fatal —
		// the per-tenant failures are already isolated inside RunOnce (SC-3).
		slog.WarnContext(ctx, "metering: pass completed with tenant failures", "err", runErr)
	}
}

// Run drives tick on the interval until ctx is cancelled. The leader gate is the
// caller's responsibility — the loop is only started on the elected leader.
func (l *Loop) Run(ctx context.Context) {
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.tick(ctx)
		}
	}
}
