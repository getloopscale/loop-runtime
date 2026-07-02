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

// TestMeteringLoop_Tick_KillSwitchEnabled_Runs — the happy path: an enabled
// kill-switch runs one pass.
func TestMeteringLoop_Tick_KillSwitchEnabled_Runs(t *testing.T) {
	runner := &fakeRunner{}
	loop := NewLoop(runner, &fakeKillSwitch{enabled: true, found: true}, &fakeMetrics{}, time.Minute)
	loop.tick(context.Background())
	if runner.calls.Load() != 1 {
		t.Errorf("RunOnce called %d times, want 1 (kill-switch enabled)", runner.calls.Load())
	}
}

// TestMeteringLoop_Tick_KillSwitchDisabled_DoesNotRun — operator-off (enabled=false,
// found=true) skips silently: no pass.
func TestMeteringLoop_Tick_KillSwitchDisabled_DoesNotRun(t *testing.T) {
	runner := &fakeRunner{}
	loop := NewLoop(runner, &fakeKillSwitch{enabled: false, found: true}, &fakeMetrics{}, time.Minute)
	loop.tick(context.Background())
	if runner.calls.Load() != 0 {
		t.Errorf("RunOnce called %d times, want 0 (kill-switch disabled)", runner.calls.Load())
	}
}

// TestMeteringLoop_Tick_KillSwitchAbsent_EC5_DoesNotRun — EC-5: an absent config
// row (found=false) means DISABLED; no pass.
func TestMeteringLoop_Tick_KillSwitchAbsent_EC5_DoesNotRun(t *testing.T) {
	runner := &fakeRunner{}
	loop := NewLoop(runner, &fakeKillSwitch{found: false}, &fakeMetrics{}, time.Minute)
	loop.tick(context.Background())
	if runner.calls.Load() != 0 {
		t.Errorf("RunOnce called on an absent kill-switch row — EC-5 violated (must default disabled)")
	}
}

// TestMeteringLoop_Tick_KillSwitchError_FailSafe_DoesNotRun — an unreadable
// kill-switch (err) HOLDS: no pass, no crash (fail-safe — never bill blind).
func TestMeteringLoop_Tick_KillSwitchError_FailSafe_DoesNotRun(t *testing.T) {
	runner := &fakeRunner{}
	loop := NewLoop(runner, &fakeKillSwitch{err: errors.New("db down")}, &fakeMetrics{}, time.Minute)
	loop.tick(context.Background()) // must not panic
	if runner.calls.Load() != 0 {
		t.Errorf("RunOnce called despite an unreadable kill-switch — fail-safe violated")
	}
}

// TestMeteringLoop_Tick_RunOncePanics_RecoveredAndCounted — a panic in RunOnce is
// recovered (never crashes the controller — ADR-1) and counted via Panic().
func TestMeteringLoop_Tick_RunOncePanics_RecoveredAndCounted(t *testing.T) {
	m := &fakeMetrics{}
	loop := NewLoop(&fakeRunner{panics: true}, &fakeKillSwitch{enabled: true, found: true}, m, time.Minute)
	loop.tick(context.Background()) // must NOT propagate the panic
	if m.panicCalls != 1 {
		t.Errorf("panicCalls=%d, want 1 (panic recovered + counted)", m.panicCalls)
	}
}

// TestMeteringLoop_Tick_RunOnceError_NoCrash — a degraded pass (RunOnce returns an
// error: some tenants failed) is logged, not fatal; the tick returns cleanly.
func TestMeteringLoop_Tick_RunOnceError_NoCrash(t *testing.T) {
	runner := &fakeRunner{err: errors.New("some tenants failed")}
	loop := NewLoop(runner, &fakeKillSwitch{enabled: true, found: true}, &fakeMetrics{}, time.Minute)
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
	loop := NewLoop(runner, &fakeKillSwitch{enabled: true, found: true}, &fakeMetrics{}, time.Minute)

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
