// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metering

// metering_loop_test.go — the leader-gated loop's per-tick control flow (plan
// metering-compute-loop T3.2, slice S4). Unit-tests tick(): kill-switch gate
// (EC-5), single-flight, and panic-recovery. The time.Ticker driver (Run) is the
// thin integration part, exercised at T6.1 LIVE.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRunner stands in for *Worker (the loop depends only on RunOnce). Thread-safe
// (atomic call counter) for the concurrent single-flight test.
type fakeRunner struct {
	calls   atomic.Int64
	err     error
	panics  bool
	started chan struct{} // buffered(1): signaled when RunOnce enters
	block   chan struct{} // if non-nil, RunOnce blocks on it before returning
}

func (f *fakeRunner) RunOnce(ctx context.Context) error {
	f.calls.Add(1)
	if f.started != nil {
		f.started <- struct{}{}
	}
	if f.panics {
		panic("metering pass boom")
	}
	if f.block != nil {
		<-f.block
	}
	return f.err
}

// fakeKillSwitch stands in for the MeteringStore kill-switch read.
type fakeKillSwitch struct {
	enabled bool
	found   bool
	err     error
}

func (f *fakeKillSwitch) IsMeteringEnabled(ctx context.Context) (bool, bool, error) {
	return f.enabled, f.found, f.err
}

// advanceableClock is a deterministic clock the freshness tests step forward
// between ticks (worker_test's fakeClock is fixed; freshness-growth needs motion).
// The loop calls tick() sequentially from one goroutine, so no lock is needed.
type advanceableClock struct{ now time.Time }

func (c *advanceableClock) Now() time.Time          { return c.now }
func (c *advanceableClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// loopEpoch is the fixed construction instant for the freshness tests (injected,
// never time.Now — determinism per rules/testing.md § 6).
var loopEpoch = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

// TestMeteringLoop_Tick_KillSwitchEnabled_Runs — the happy path: an enabled
// kill-switch runs one pass.
func TestMeteringLoop_Tick_KillSwitchEnabled_Runs(t *testing.T) {
	runner := &fakeRunner{}
	loop := NewLoop(runner, &fakeKillSwitch{enabled: true, found: true}, &fakeMetrics{}, fakeClock{}, time.Minute)
	loop.tick(context.Background())
	if runner.calls.Load() != 1 {
		t.Errorf("RunOnce called %d times, want 1 (kill-switch enabled)", runner.calls.Load())
	}
}

// TestMeteringLoop_Tick_KillSwitchDisabled_DoesNotRun — operator-off (enabled=false,
// found=true) skips silently: no pass.
func TestMeteringLoop_Tick_KillSwitchDisabled_DoesNotRun(t *testing.T) {
	runner := &fakeRunner{}
	loop := NewLoop(runner, &fakeKillSwitch{enabled: false, found: true}, &fakeMetrics{}, fakeClock{}, time.Minute)
	loop.tick(context.Background())
	if runner.calls.Load() != 0 {
		t.Errorf("RunOnce called %d times, want 0 (kill-switch disabled)", runner.calls.Load())
	}
}

// TestMeteringLoop_Tick_KillSwitchAbsent_EC5_DoesNotRun — EC-5: an absent config
// row (found=false) means DISABLED; no pass.
func TestMeteringLoop_Tick_KillSwitchAbsent_EC5_DoesNotRun(t *testing.T) {
	runner := &fakeRunner{}
	loop := NewLoop(runner, &fakeKillSwitch{found: false}, &fakeMetrics{}, fakeClock{}, time.Minute)
	loop.tick(context.Background())
	if runner.calls.Load() != 0 {
		t.Errorf("RunOnce called on an absent kill-switch row — EC-5 violated (must default disabled)")
	}
}

// TestMeteringLoop_Tick_KillSwitchError_FailSafe_DoesNotRun — an unreadable
// kill-switch (err) HOLDS: no pass, no crash (fail-safe — never bill blind).
func TestMeteringLoop_Tick_KillSwitchError_FailSafe_DoesNotRun(t *testing.T) {
	runner := &fakeRunner{}
	loop := NewLoop(runner, &fakeKillSwitch{err: errors.New("db down")}, &fakeMetrics{}, fakeClock{}, time.Minute)
	loop.tick(context.Background()) // must not panic
	if runner.calls.Load() != 0 {
		t.Errorf("RunOnce called despite an unreadable kill-switch — fail-safe violated")
	}
}

// TestMeteringLoop_Tick_RunOncePanics_RecoveredAndCounted — a panic in RunOnce is
// recovered (never crashes the controller — ADR-1) and counted via Panic().
func TestMeteringLoop_Tick_RunOncePanics_RecoveredAndCounted(t *testing.T) {
	m := &fakeMetrics{}
	loop := NewLoop(&fakeRunner{panics: true}, &fakeKillSwitch{enabled: true, found: true}, m, fakeClock{}, time.Minute)
	loop.tick(context.Background()) // must NOT propagate the panic
	if m.panicCalls != 1 {
		t.Errorf("panicCalls=%d, want 1 (panic recovered + counted)", m.panicCalls)
	}
}

// TestMeteringLoop_Tick_RunOnceError_NoCrash — a degraded pass (RunOnce returns an
// error: some tenants failed) is logged, not fatal; the tick returns cleanly.
func TestMeteringLoop_Tick_RunOnceError_NoCrash(t *testing.T) {
	runner := &fakeRunner{err: errors.New("some tenants failed")}
	loop := NewLoop(runner, &fakeKillSwitch{enabled: true, found: true}, &fakeMetrics{}, fakeClock{}, time.Minute)
	loop.tick(context.Background())
	if runner.calls.Load() != 1 {
		t.Errorf("RunOnce should still run + return cleanly on a degraded pass, calls=%d", runner.calls.Load())
	}
}

// TestMeteringLoop_Tick_SingleFlight_OverlapSkipped — a slow pass holding the
// single-flight lock makes a concurrent tick skip RunOnce (no overlapping pass).
func TestMeteringLoop_Tick_SingleFlight_OverlapSkipped(t *testing.T) {
	release := make(chan struct{})
	runner := &fakeRunner{started: make(chan struct{}, 1), block: release}
	loop := NewLoop(runner, &fakeKillSwitch{enabled: true, found: true}, &fakeMetrics{}, fakeClock{}, time.Minute)

	done := make(chan struct{})
	go func() { loop.tick(context.Background()); close(done) }() // first tick — blocks in RunOnce
	<-runner.started                                             // it acquired the single-flight lock

	loop.tick(context.Background()) // second tick — must skip (busy)
	if got := runner.calls.Load(); got != 1 {
		t.Errorf("RunOnce called %d times during an in-flight pass, want 1 (single-flight must skip the overlap)", got)
	}

	close(release) // let the first pass finish
	<-done
	if got := runner.calls.Load(); got != 1 {
		t.Errorf("RunOnce called %d times total, want 1", got)
	}
}

// TestMeteringLoop_Tick_EnabledPass_RecordsDurationAndFreshness — a completed pass
// records its wall-clock (pass_duration_seconds, once) and anchors freshness
// (seconds_since_last_success set once, at tick start, to the staleness before the
// pass). S4b: the two per-pass observability metrics fire on the happy path.
func TestMeteringLoop_Tick_EnabledPass_RecordsDurationAndFreshness(t *testing.T) {
	m := &fakeMetrics{}
	clk := &advanceableClock{now: loopEpoch}
	loop := NewLoop(&fakeRunner{}, &fakeKillSwitch{enabled: true, found: true}, m, clk, time.Minute)

	clk.advance(30 * time.Second) // 30s since construction before the pass runs
	loop.tick(context.Background())

	if len(m.passDurations) != 1 {
		t.Fatalf("pass_duration recorded %d times, want 1 (a completed pass is timed)", len(m.passDurations))
	}
	if m.freshnessCalls != 1 {
		t.Errorf("freshness set %d times, want 1 (set once per tick at start)", m.freshnessCalls)
	}
	if m.lastFreshness != 30 {
		t.Errorf("freshness=%v, want 30 (30s elapsed since the construction anchor before this pass)", m.lastFreshness)
	}
}

// TestMeteringLoop_Tick_RepeatedPanic_FreshnessGrows — a repeatedly-panicking loop
// never updates lastSuccess (the panic returns before the completion anchor), so
// seconds_since_last_success grows tick over tick → the >3×interval alert fires.
// pass_duration is NOT recorded on a panic (no pass completed).
func TestMeteringLoop_Tick_RepeatedPanic_FreshnessGrows(t *testing.T) {
	m := &fakeMetrics{}
	clk := &advanceableClock{now: loopEpoch}
	loop := NewLoop(&fakeRunner{panics: true}, &fakeKillSwitch{enabled: true, found: true}, m, clk, time.Minute)

	clk.advance(time.Minute)
	loop.tick(context.Background()) // panics; lastSuccess frozen at construction
	first := m.lastFreshness

	clk.advance(time.Minute)
	loop.tick(context.Background()) // panics again; freshness larger (lastSuccess still frozen)
	second := m.lastFreshness

	if !(second > first) {
		t.Errorf("freshness must grow across repeated panics (lastSuccess frozen): first=%v second=%v", first, second)
	}
	if m.panicCalls != 2 {
		t.Errorf("panicCalls=%d, want 2", m.panicCalls)
	}
	if len(m.passDurations) != 0 {
		t.Errorf("no pass completed under panic → no pass_duration, got %d", len(m.passDurations))
	}
}

// TestMeteringLoop_Tick_Disabled_FreshnessDoesNotGrow — a deliberately-off loop
// (kill-switch disabled) must NOT accumulate staleness: each idle tick keep-alives
// lastSuccess so the freshness gauge holds ~interval and never trips the alert. Off
// by config ≠ stuck.
func TestMeteringLoop_Tick_Disabled_FreshnessDoesNotGrow(t *testing.T) {
	m := &fakeMetrics{}
	clk := &advanceableClock{now: loopEpoch}
	loop := NewLoop(&fakeRunner{}, &fakeKillSwitch{enabled: false, found: true}, m, clk, time.Minute)

	clk.advance(time.Minute)
	loop.tick(context.Background())
	first := m.lastFreshness

	clk.advance(time.Minute)
	loop.tick(context.Background())
	second := m.lastFreshness

	if second > first {
		t.Errorf("a disabled loop must not accumulate staleness (keep-alive): first=%v second=%v", first, second)
	}
}

// freshnessAfterTwoTicks runs two ticks a minute apart and returns the
// freshness observed at the start of each.
func freshnessAfterTwoTicks(r runner, ks killSwitch) (first, second float64) {
	m := &fakeMetrics{}
	clk := &advanceableClock{now: loopEpoch}
	loop := NewLoop(r, ks, m, clk, time.Minute)
	clk.advance(time.Minute)
	loop.tick(context.Background())
	clk.advance(time.Minute)
	loop.tick(context.Background())
	first = m.lastFreshness
	clk.advance(time.Minute)
	loop.tick(context.Background())
	return first, m.lastFreshness
}

// An unreadable kill-switch is an outage (the database is unreachable), not an
// idle loop: no pass metered, so billing staleness must grow and alert.
func TestMeteringLoop_Tick_KillSwitchError_FreshnessGrows(t *testing.T) {
	first, second := freshnessAfterTwoTicks(&fakeRunner{}, &fakeKillSwitch{err: errors.New("db down")})
	if second <= first {
		t.Errorf("freshness must grow while the kill-switch is unreadable: first=%v second=%v", first, second)
	}
}

// A deleted kill-switch row is an operational alarm (EC-5), not a deliberate off.
func TestMeteringLoop_Tick_KillSwitchRowAbsent_FreshnessGrows(t *testing.T) {
	first, second := freshnessAfterTwoTicks(&fakeRunner{}, &fakeKillSwitch{found: false})
	if second <= first {
		t.Errorf("freshness must grow while the kill-switch row is absent: first=%v second=%v", first, second)
	}
}

// A pass in which every tenant failed metered nothing.
func TestMeteringLoop_Tick_EveryTenantFailed_FreshnessGrows(t *testing.T) {
	runner := &fakeRunner{err: &PassError{Failed: 2, Total: 2, Err: errors.New("cells unreachable")}}
	first, second := freshnessAfterTwoTicks(runner, &fakeKillSwitch{enabled: true, found: true})
	if second <= first {
		t.Errorf("freshness must grow when every tenant failed: first=%v second=%v", first, second)
	}
}

// A pass that could not even list the tenants metered nothing either.
func TestMeteringLoop_Tick_ListingFailed_FreshnessGrows(t *testing.T) {
	runner := &fakeRunner{err: errors.New("metering: list active tenants: db down")}
	first, second := freshnessAfterTwoTicks(runner, &fakeKillSwitch{enabled: true, found: true})
	if second <= first {
		t.Errorf("freshness must grow when the tenant list failed: first=%v second=%v", first, second)
	}
}

// A degraded pass in which some tenants were metered keeps the loop fresh.
func TestMeteringLoop_Tick_SomeTenantsMetered_FreshnessHolds(t *testing.T) {
	runner := &fakeRunner{err: &PassError{Failed: 1, Total: 2, Err: errors.New("one cell unreachable")}}
	first, second := freshnessAfterTwoTicks(runner, &fakeKillSwitch{enabled: true, found: true})
	if second > first {
		t.Errorf("a partially successful pass must keep freshness: first=%v second=%v", first, second)
	}
}
