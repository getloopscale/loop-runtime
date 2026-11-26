// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/getloopscale/loop-runtime/api/internal/surfaces"
)

// SurfaceEnabled reports, per HTTP surface, whether the router registered it.
//
// 1 = registered and reachable. 0 = skipped, because a dependency the surface
// needs was absent at startup.
//
// # Why a gauge and not a log line
//
// The router already decides fifty times whether to register a surface, and
// until B-104 every one of those decisions vanished into an `Info` line. Measured
// on the droplet: with `GITHUB_WEBHOOK_SECRET` absent the webhook route answered
// 404, and nothing anywhere said the surface was off. A gauge is the channel
// operations already watches, so a disabled surface can raise an alert instead of
// waiting to be noticed by a customer whose `git push` built nothing.
//
// # Why enabled surfaces emit 1 rather than staying absent
//
// Absent and zero are indistinguishable to whoever writes the alert. Emitting the
// series in both states is what makes `== 0` a statement about the surface rather
// than a statement about which build is running.
//
// # Why the reason is not a label
//
// The reason is free-form text naming the missing dependency. Free-form label
// values are unbounded cardinality, and unbounded cardinality is how a collector
// falls over — this repository already bounds every label it ships (see the
// deploy-timeline metrics above). The reason belongs in the startup summary,
// which is read once by a human, not stored once per series forever.
var SurfaceEnabled = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "loopruntime_api_surface_enabled",
	Help: "1 when the router registered this HTTP surface, 0 when a missing dependency made it skip it.",
}, []string{"surface"})

// PublishSurfaces mirrors the router's registration decisions onto the gauge.
//
// It is called once, after the router is built and before serving begins. A nil
// or empty slice is a silent no-op: a binary that recorded no surface is
// implausible, but crashing at startup over it would trade the blind spot this
// package removes for an outage.
func PublishSurfaces(all []surfaces.Surface) {
	for _, s := range all {
		value := 0.0
		if s.Enabled {
			value = 1.0
		}
		SurfaceEnabled.WithLabelValues(s.Name).Set(value)
	}
}
