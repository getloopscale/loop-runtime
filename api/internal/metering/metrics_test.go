// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metering

// metrics_test.go — the production Prometheus metering.Metrics impl (plan
// metering-compute-loop T3.2, slice S1). Only the counters with a live worker
// caller land here (YAGNI): Advanced → cpu_core_seconds_written_total (the
// wiring-triad / pillar-c metric), CellError → cell_errors_total. The remaining
// canonical loopruntime_metering_* counters land WITH their callers (the loop, S4).

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
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

// TestPrometheusMetrics_Panic_IncrementsPanicsTotal — Panic() counts a recovered
// metering-pass panic (ADR-1 — the loop never crashes the controller).
func TestPrometheusMetrics_Panic_IncrementsPanicsTotal(t *testing.T) {
	m := NewPrometheusMetrics()
	before := testutil.ToFloat64(panicsTotal)
	m.Panic()
	if got := testutil.ToFloat64(panicsTotal) - before; got != 1 {
		t.Errorf("panics_total delta=%v, want 1", got)
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

// TestPrometheusMetrics_TenantMetered_IncrementsCounter — TenantMetered() adds one
// to tenants_metered_total (S4b subset-miss detector: compared against the active
// gauge, a lagging rate exposes a silently-skipped slice of the fleet).
func TestPrometheusMetrics_TenantMetered_IncrementsCounter(t *testing.T) {
	m := NewPrometheusMetrics()
	before := testutil.ToFloat64(tenantsMeteredTotal)
	m.TenantMetered()
	if got := testutil.ToFloat64(tenantsMeteredTotal) - before; got != 1 {
		t.Errorf("tenants_metered_total delta=%v, want 1", got)
	}
}

// TestPrometheusMetrics_PassDuration_ObservesHistogram — PassDuration(s) observes
// one sample into pass_duration_seconds (the informer-ceiling histogram). A
// histogram exposes multiple series, so ToFloat64 does not apply — the sample
// count is read via the dto write.
func TestPrometheusMetrics_PassDuration_ObservesHistogram(t *testing.T) {
	m := NewPrometheusMetrics()
	before := histSampleCount(t)
	m.PassDuration(3.5)
	if got := histSampleCount(t) - before; got != 1 {
		t.Errorf("pass_duration_seconds sample count delta=%d, want 1", got)
	}
}

// TestPrometheusMetrics_SecondsSinceLastSuccess_SetsGauge — SecondsSinceLastSuccess(s)
// SETS (not adds) the freshness gauge to the absolute staleness value.
func TestPrometheusMetrics_SecondsSinceLastSuccess_SetsGauge(t *testing.T) {
	m := NewPrometheusMetrics()
	m.SecondsSinceLastSuccess(123)
	if got := testutil.ToFloat64(secondsSinceLastSuccess); got != 123 {
		t.Errorf("seconds_since_last_success=%v, want 123 (Set, not Add)", got)
	}
	m.SecondsSinceLastSuccess(7) // a Set overwrites, never accumulates
	if got := testutil.ToFloat64(secondsSinceLastSuccess); got != 7 {
		t.Errorf("seconds_since_last_success=%v after re-Set, want 7", got)
	}
}

// histSampleCount reads the cumulative observation count of the pass-duration
// histogram (the promauto Histogram exposes a series count, not a scalar, so the
// dto write is the way to assert an Observe landed).
func histSampleCount(t *testing.T) uint64 {
	t.Helper()
	var metric dto.Metric
	if err := passDurationSeconds.(interface {
		Write(*dto.Metric) error
	}).Write(&metric); err != nil {
		t.Fatalf("write pass_duration_seconds: %v", err)
	}
	return metric.GetHistogram().GetSampleCount()
}
