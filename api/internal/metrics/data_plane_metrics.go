// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Data-plane readiness poller (environments.ReadinessPoller): the snapshots
// behind the dashboard's project badge. A snapshot that stops being written
// shows as a badge that degrades to "Unknown"; these say why.
var (
	// DataPlaneReadinessSnapshots counts readiness samples by what became of
	// them: recorded, no_target (no environment_targets row for the tuple),
	// or failed (the write errored).
	DataPlaneReadinessSnapshots = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_data_plane_readiness_snapshots_total",
		Help: "Data-plane readiness samples by outcome (recorded|no_target|failed).",
	}, []string{"result"})

	// DataPlaneReadinessScrapeFailures counts poller ticks whose Prometheus
	// query failed; every stored snapshot keeps its last value.
	DataPlaneReadinessScrapeFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_data_plane_readiness_scrape_failures_total",
		Help: "Readiness poller ticks whose Prometheus query failed.",
	})
)
