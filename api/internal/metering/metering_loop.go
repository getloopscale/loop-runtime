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

// killSwitch is the narrow kill-switch read (satisfied by *db.MeteringStore).
type killSwitch interface {
	IsMeteringEnabled(ctx context.Context) (enabled bool, found bool, err error)
}

// Loop drives one metering pass per tick until its context is cancelled.
type Loop struct {
	runner     runner
	killSwitch killSwitch
	metrics    Metrics
	interval   time.Duration
	running    atomic.Bool // single-flight: true while a pass is in flight
}

// NewLoop wires the loop's collaborators + tick interval.
func NewLoop(r runner, ks killSwitch, m Metrics, interval time.Duration) *Loop {
	return &Loop{runner: r, killSwitch: ks, metrics: m, interval: interval}
}

// tick runs one metering pass, gated by the kill-switch + single-flight, inside a
// panic-recovery boundary. It NEVER returns an error and NEVER propagates a panic:
// a pass failure is observed via metrics + logs, and the controller keeps running
// (ADR-1). The order is single-flight → recover → kill-switch → RunOnce.
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

	enabled, found, err := l.killSwitch.IsMeteringEnabled(ctx)
	if err != nil {
		// EC-5 fail-safe: an unreadable kill-switch HOLDS — never bill blind.
		slog.WarnContext(ctx, "metering: kill-switch unreadable — skipping pass (fail-safe)", "err", err)
		return
	}
	if !found {
		// EC-5: the config row is absent → DISABLED + WARN (operational alarm — a
		// seeded row was deleted).
		slog.WarnContext(ctx, "metering: kill-switch row absent — metering disabled (EC-5)")
		return
	}
	if !enabled {
		// Normal operator-off state — observe-only ships disabled. Skip silently.
		return
	}

	if err := l.runner.RunOnce(ctx); err != nil {
		// A degraded pass (one or more tenants failed) is logged, never fatal —
		// the per-tenant failures are already isolated inside RunOnce (SC-3).
		slog.WarnContext(ctx, "metering: pass completed with tenant failures", "err", err)
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
