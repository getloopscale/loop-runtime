// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Deploy Timeline metrics (plan T5.2 — 4 canonical counters/histograms).
//
// Naming: loop_runtime_deploy_timeline_<name>. Labels bounded cardinality:
//   - phase ∈ {build, sign, release, deploy, live}
//   - status ∈ DeployTimelinePhaseStatus (≤6 values)
//   - source ∈ SourceID (≤5 values)
//   - reason ∈ free-form but cardinality bounded by error class
var (
	// DeployTimelineEventsTotal counts events emitted by the aggregator,
	// labeled by phase + status + source. Used to confirm fan-in is
	// working and detect silent source-readers per ADR D6.
	DeployTimelineEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loop_runtime_deploy_timeline_events_total",
		Help: "Total deploy timeline events emitted by the aggregator.",
	}, []string{"phase", "status", "source"})

	// DeployTimelinePhaseDuration measures wall-clock time per phase
	// (build → sign → release → deploy → live). Buckets cover the
	// canonical 4-7 minute end-to-end deploy window.
	DeployTimelinePhaseDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loop_runtime_deploy_timeline_phase_duration_seconds",
		Help:    "Wall-clock duration of each canonical phase.",
		Buckets: []float64{1, 5, 10, 30, 60, 120, 180, 300, 600, 900},
	}, []string{"phase"})

	// DeployTimelineScrubReplacementsTotal counts denylist replacements
	// the scrub layer made before persistence. Useful as a leakage SLI:
	// if this jumps suddenly, upstream is emitting more internal-term
	// text (Cross-Project Rule 9 health signal).
	DeployTimelineScrubReplacementsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loop_runtime_deploy_timeline_scrub_replacements_total",
		Help: "Total internal-term replacements applied by the scrub layer.",
	}, []string{"phase"})

	// DeployTimelineAggregatorPanicTotal counts goroutine panics caught
	// by the aggregator. A non-zero rate triggers an alert per ADR D1
	// § Drawbacks (each panic is one deploy that lost real-time signal).
	DeployTimelineAggregatorPanicTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loop_runtime_deploy_timeline_aggregator_panic_total",
		Help: "Total aggregator/adapter panics recovered.",
	}, []string{"source"})
)
