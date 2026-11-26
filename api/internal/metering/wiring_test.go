// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metering

// wiring_test.go — the production assembly constructor (plan
// metering-compute-loop T3.2, slice S5). Unit-tests that NewWorkerLoop threads
// the lease fence + tunables into a wired Loop. Construction touches no I/O (the
// pool is used lazily), so a nil pool + nil CR client are fine here; the live
// cell/pool behaviour is exercised at T6.1 LIVE.

import (
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/leader"
)

// TestNewWorkerLoop_ThreadsTunablesAndLease — the assembly wires the lease
// (SC-6 fence), MaxDelta (ADR-8 clamp), MaxConcurrency, and interval into the
// Worker + Loop. Asserts the [CRITICAL] guardrail: MaxDelta MUST be < one billing
// period (ADR-8 / SC-7), else a stale-cursor catch-up could span two periods.
func TestNewWorkerLoop_ThreadsTunablesAndLease(t *testing.T) {
	lease := leader.Lease{Name: "compute-metering", CellID: "cell-0", GenerationID: 7}
	loop := NewWorkerLoop(nil, nil, &fakeCellClientProvider{}, LoopConfig{
		Interval:       time.Minute,
		MaxDelta:       time.Hour,
		MaxConcurrency: 4,
		Lease:          lease,
	})
	if loop == nil {
		t.Fatal("NewWorkerLoop returned nil")
	}
	if loop.interval != time.Minute {
		t.Errorf("loop interval=%v, want 1m", loop.interval)
	}

	w, ok := loop.runner.(*Worker)
	if !ok {
		t.Fatalf("loop.runner is %T, want *Worker", loop.runner)
	}
	if w.Lease.GenerationID != 7 {
		t.Errorf("Worker.Lease.GenerationID=%d, want 7 (SC-6 fence threaded)", w.Lease.GenerationID)
	}
	if w.MaxDelta != time.Hour {
		t.Errorf("Worker.MaxDelta=%v, want 1h", w.MaxDelta)
	}
	if w.MaxConcurrency != 4 {
		t.Errorf("Worker.MaxConcurrency=%d, want 4", w.MaxConcurrency)
	}
	// [CRITICAL guardrail] MaxDelta < one billing period (ADR-8 / SC-7).
	if w.MaxDelta >= 28*24*time.Hour {
		t.Errorf("Worker.MaxDelta=%v spans a billing period — ADR-8/SC-7 violated", w.MaxDelta)
	}
	if w.Store == nil || w.Namespaces == nil || w.PodListers == nil || w.Metrics == nil || w.Clock == nil || w.Lister == nil {
		t.Error("Worker collaborators not fully wired")
	}
	// The kill-switch the loop reads is the same MeteringStore that backs the worker.
	if loop.killSwitch == nil {
		t.Error("loop.killSwitch not wired")
	}
}

// TestSystemClock_NowIsRecent — the production Clock returns the real wall clock.
func TestSystemClock_NowIsRecent(t *testing.T) {
	before := time.Now()
	got := SystemClock{}.Now()
	if got.Before(before) || got.After(before.Add(time.Second)) {
		t.Errorf("SystemClock.Now()=%v, want ~%v", got, before)
	}
}
