// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metering

// metrics.go — the production Prometheus implementation of the metering.Metrics
// seam (plan metering-compute-loop T3.2, slice S1). Only the counters with a live
// worker caller are declared here (YAGNI — the remaining canonical loop_runtime_metering_*
// counters land WITH their callers in the loop, S4). Package-global promauto vars
// on the default registerer, matching the repo convention (api/internal/metrics).

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// cpuCoreSecondsWrittenTotal is the wiring-triad metric (pillar c): it fires
	// non-zero the moment the worker meters a real tenant's reserved compute.
	cpuCoreSecondsWrittenTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loop_runtime_metering_cpu_core_seconds_written_total",
		Help: "Total cpu_core_seconds the metering worker has written to cost_counters.",
	})

	// cellErrorsTotal counts cell/collect read failures that HELD a tenant's
	// cursor this tick (SC-3 — a read failure is never swallowed to Σreq=0).
	cellErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loop_runtime_metering_cell_errors_total",
		Help: "Total cell/collect read failures that held a tenant's metering cursor (SC-3).",
	})

	// panicsTotal counts metering passes that panicked and were recovered by the
	// loop's panic-recovery boundary (ADR-1 — a panic must never crash the
	// controller). A non-zero value is an alarm: the pass is not completing.
	panicsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loop_runtime_metering_panics_total",
		Help: "Total metering passes that panicked and were recovered (ADR-1).",
	})

	// tenantsMeteredTotal counts individual tenants successfully metered. Compared
	// against the active-tenant count (loop_runtime_tenants_active), a persistent shortfall
	// in rate(tenants_metered_total) is the subset-miss detector: a bug that silently
	// skips a slice of the fleet leaves this counter lagging the active gauge.
	tenantsMeteredTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loop_runtime_metering_tenants_metered_total",
		Help: "Total tenants successfully metered (vs loop_runtime_tenants_active — subset-miss detector).",
	})

	// passDurationSeconds is the wall-clock of one RunOnce pass. Its p95 approaching
	// the tick interval is the informer-ceiling signal (the fleet no longer fits in
	// one interval → widen the interval or shard the loop). Buckets span sub-second
	// (tiny fleets) to minutes (1500-tenant load).
	passDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "loop_runtime_metering_pass_duration_seconds",
		Help:    "Wall-clock duration of one metering pass (informer-ceiling signal).",
		Buckets: []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
	})

	// secondsSinceLastSuccess is the freshness gauge: seconds elapsed since the last
	// completed pass, refreshed each tick. Alerts fire when it exceeds ~3×interval
	// (the loop is stuck: stalled RunOnce, lost leadership without release, wedged
	// informer). A stale metering loop under-bills silently — this gauge is the alarm.
	secondsSinceLastSuccess = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loop_runtime_metering_seconds_since_last_success",
		Help: "Seconds since the last completed metering pass (freshness — alert on >3×interval).",
	})
)

// PrometheusMetrics is the production metering.Metrics, backed by the package
// counters above. Stateless — the counters are process-global.
type PrometheusMetrics struct{}

// NewPrometheusMetrics returns the production metrics recorder.
func NewPrometheusMetrics() *PrometheusMetrics { return &PrometheusMetrics{} }

// Advanced adds delta cpu_core_seconds to the wiring-triad counter.
func (PrometheusMetrics) Advanced(delta int64) {
	cpuCoreSecondsWrittenTotal.Add(float64(delta))
}

// CellError counts a cell/collect read failure (SC-3 cursor HOLD).
func (PrometheusMetrics) CellError() {
	cellErrorsTotal.Inc()
}

// Panic counts a recovered metering-pass panic (loop_runtime_metering_panics_total).
func (PrometheusMetrics) Panic() {
	panicsTotal.Inc()
}

// Held is a no-op: a HELD write (lost CAS / backward clock / period straddle /
// stale fence) has no dedicated counter in the plan's canonical loop_runtime_metering_*
// list. The loop's tenants_metered_total vs the active-tenant count is the
// subset-miss signal that surfaces persistent holds; a per-hold counter would be
// an invented metric (rejected — do not invent counters the plan did not declare).
func (PrometheusMetrics) Held() {}

// TenantMetered increments the per-tenant success counter (subset-miss detector).
func (PrometheusMetrics) TenantMetered() {
	tenantsMeteredTotal.Inc()
}

// PassDuration observes one pass's wall-clock (informer-ceiling histogram).
func (PrometheusMetrics) PassDuration(seconds float64) {
	passDurationSeconds.Observe(seconds)
}

// SecondsSinceLastSuccess sets the freshness gauge (staleness alarm).
func (PrometheusMetrics) SecondsSinceLastSuccess(s float64) {
	secondsSinceLastSuccess.Set(s)
}
