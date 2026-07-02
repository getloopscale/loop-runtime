// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metering

// metrics_test.go — the production Prometheus metering.Metrics impl (plan
// metering-compute-loop T3.2, slice S1). Only the counters with a live worker
// caller land here (YAGNI): Advanced → cpu_core_seconds_written_total (the
// wiring-triad / pillar-c metric), CellError → cell_errors_total. The remaining
// canonical loopzero_metering_* counters land WITH their callers (the loop, S4).

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestPrometheusMetrics_SatisfiesInterface — compile-time proof the concrete impl
// is a metering.Metrics (the seam the worker already depends on).
func TestPrometheusMetrics_SatisfiesInterface(t *testing.T) {
	var _ Metrics = (*PrometheusMetrics)(nil)
}

// TestPrometheusMetrics_Advanced_IncrementsCPUCoreSecondsWritten — Advanced(delta)
// adds delta cpu_core_seconds to the wiring-triad counter (pillar c: this is the
// metric that fires non-zero once the worker meters a real tenant).
func TestPrometheusMetrics_Advanced_IncrementsCPUCoreSecondsWritten(t *testing.T) {
	m := NewPrometheusMetrics()
	before := testutil.ToFloat64(cpuCoreSecondsWrittenTotal)
	m.Advanced(42)
	if got := testutil.ToFloat64(cpuCoreSecondsWrittenTotal) - before; got != 42 {
		t.Errorf("cpu_core_seconds_written delta=%v, want 42", got)
	}
}

// TestPrometheusMetrics_CellError_IncrementsCellErrors — CellError() counts a
// cell/collect read failure that HELD a tenant's cursor (SC-3).
func TestPrometheusMetrics_CellError_IncrementsCellErrors(t *testing.T) {
	m := NewPrometheusMetrics()
	before := testutil.ToFloat64(cellErrorsTotal)
	m.CellError()
	if got := testutil.ToFloat64(cellErrorsTotal) - before; got != 1 {
		t.Errorf("cell_errors delta=%v, want 1", got)
	}
}

// TestPrometheusMetrics_Held_IsNoOpNotPanic — Held() has no canonical counter in
// the plan's list; the impl is a documented no-op (must not panic, must not
// touch the two real counters).
func TestPrometheusMetrics_Held_IsNoOpNotPanic(t *testing.T) {
	m := NewPrometheusMetrics()
	cpuBefore := testutil.ToFloat64(cpuCoreSecondsWrittenTotal)
	cellBefore := testutil.ToFloat64(cellErrorsTotal)
	m.Held() // must not panic
	if testutil.ToFloat64(cpuCoreSecondsWrittenTotal) != cpuBefore {
		t.Error("Held touched cpu_core_seconds_written_total, want no-op")
	}
	if testutil.ToFloat64(cellErrorsTotal) != cellBefore {
		t.Error("Held touched cell_errors_total, want no-op")
	}
}
