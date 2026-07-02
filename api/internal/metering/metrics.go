// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metering

// metrics.go — the production Prometheus implementation of the metering.Metrics
// seam (plan metering-compute-loop T3.2, slice S1). Only the counters with a live
// worker caller are declared here (YAGNI — the remaining canonical loopzero_metering_*
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
		Name: "loopzero_metering_cpu_core_seconds_written_total",
		Help: "Total cpu_core_seconds the metering worker has written to cost_counters.",
	})

	// cellErrorsTotal counts cell/collect read failures that HELD a tenant's
	// cursor this tick (SC-3 — a read failure is never swallowed to Σreq=0).
	cellErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_metering_cell_errors_total",
		Help: "Total cell/collect read failures that held a tenant's metering cursor (SC-3).",
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

// Held is a no-op: a HELD write (lost CAS / backward clock / period straddle /
// stale fence) has no dedicated counter in the plan's canonical loopzero_metering_*
// list. The loop's tenants_metered_total vs the active-tenant count (S4) is the
// subset-miss signal that surfaces persistent holds; a per-hold counter would be
// an invented metric (rejected — do not invent counters the plan did not declare).
func (PrometheusMetrics) Held() {}
