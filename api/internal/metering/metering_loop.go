// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package metering measures tenant resource usage and turns it into the counters
// billing reads.
//
// Its defining constraint is that it runs beside the control plane without being
// able to disturb it: the loop is leader-gated, single-flighted so two passes never
// overlap, wrapped in a panic-recovery boundary (ADR-1 — a metering panic must
// never crash the controller), and gated by a kill-switch. Everything here is
// written so that failing to measure is always preferable to interrupting what is
// being measured.
package metering

// metering_loop.go — the leader-gated driver of worker.RunOnce (plan
// metering-compute-loop T3.2, slice S4). The loop is started only on the leader
// (the caller's responsibility); each tick is gated by the kill-switch (EC-5),
// guarded by single-flight (no overlapping pass), and wrapped in a panic-recovery
// boundary (ADR-1 — a metering panic must never crash the controller).

import (
	"context"
	"errors"
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
		// EC-5 fail-safe: an unreadable kill-switch HOLDS — never bill blind. It is
		// an outage (the store is unreachable), not idleness: lastSuccess is NOT
		// advanced, so the freshness gauge grows and alerts on the billing gap.
		slog.WarnContext(ctx, "metering: kill-switch unreadable — skipping pass (fail-safe)", "err", err)
		return
	}
	if !found {
		// EC-5: the config row is absent → DISABLED + WARN. A seeded row was
		// deleted — an operational alarm, so freshness is left to grow.
		slog.WarnContext(ctx, "metering: kill-switch row absent — metering disabled (EC-5)")
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
	l.metrics.PassDuration(l.clock.Now().Sub(start).Seconds())
	// Freshness is anchored only by a pass that metered something: a clean pass,
	// or a degraded one in which some tenants were metered (per-tenant failures
	// are isolated inside RunOnce, SC-3). A pass that failed for every tenant, or
	// could not list them, leaves lastSuccess frozen — the alarm signal. A panic
	// returns above without reaching here, with the same effect.
	if runErr == nil || meteredSome(runErr) {
		l.lastSuccess = l.clock.Now()
	}
	if runErr != nil {
		slog.WarnContext(ctx, "metering: pass completed with failures", "err", runErr)
	}
}

// meteredSome reports whether a failed pass still metered at least one tenant.
func meteredSome(err error) bool {
	var pe *PassError
	return errors.As(err, &pe) && pe.Failed < pe.Total
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
