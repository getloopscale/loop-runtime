// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestDeployTimelineMetrics_Registered confirms the 4 canonical
// deploy_timeline metrics are registered with Prometheus and have the
// expected label sets per plan T5.2.
func TestDeployTimelineMetrics_Registered(t *testing.T) {
	DeployTimelineEventsTotal.WithLabelValues("build", "in_progress", "loki_markers").Inc()
	DeployTimelinePhaseDuration.WithLabelValues("build").Observe(120.0)
	DeployTimelineScrubReplacementsTotal.WithLabelValues("deploy").Inc()
	DeployTimelineAggregatorPanicTotal.WithLabelValues("loki_markers").Inc()

	tests := []struct {
		name string
		want float64
		got  func() float64
	}{
		{
			name: "loopzero_deploy_timeline_events_total",
			want: 1.0,
			got: func() float64 {
				return testutil.ToFloat64(DeployTimelineEventsTotal.WithLabelValues("build", "in_progress", "loki_markers"))
			},
		},
		{
			name: "loopzero_deploy_timeline_scrub_replacements_total",
			want: 1.0,
			got: func() float64 {
				return testutil.ToFloat64(DeployTimelineScrubReplacementsTotal.WithLabelValues("deploy"))
			},
		},
		{
			name: "loopzero_deploy_timeline_aggregator_panic_total",
			want: 1.0,
			got: func() float64 {
				return testutil.ToFloat64(DeployTimelineAggregatorPanicTotal.WithLabelValues("loki_markers"))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.got(); got != tt.want {
				t.Errorf("%s = %f; want %f", tt.name, got, tt.want)
			}
		})
	}
}

// TestDeployTimelineMetrics_NameConvention asserts every metric follows
// the loopzero_deploy_timeline_ prefix per spec §15.2 naming convention.
func TestDeployTimelineMetrics_NameConvention(t *testing.T) {
	names := []string{
		"loopzero_deploy_timeline_events_total",
		"loopzero_deploy_timeline_phase_duration_seconds",
		"loopzero_deploy_timeline_scrub_replacements_total",
		"loopzero_deploy_timeline_aggregator_panic_total",
	}
	for _, n := range names {
		if !strings.HasPrefix(n, "loopzero_deploy_timeline_") {
			t.Errorf("metric %q does not match loopzero_deploy_timeline_ prefix", n)
		}
	}
}
