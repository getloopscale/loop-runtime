// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/config"
)

// The Status page's metrics are queried by the API itself, so they need the Prometheus
// the API pod reaches. LOOPRUNTIME_PROMETHEUS_ADDRESS is the one canary analysis queries
// from the cells' pods; on staging that is another name on another cluster, and the
// metrics answered 503 prometheus_not_configured with the core Prometheus up (#102).
func TestTheStatusMetricsQueryThePrometheusTheAPIReaches(t *testing.T) {
	cfg := config.Config{
		PrometheusAddress:            "http://cell-prometheus.example.test",
		DataPlanePollerPrometheusURL: "http://core-prometheus.example.test",
	}

	if got := metricsPrometheusURL(cfg); got != "http://core-prometheus.example.test" {
		t.Errorf("metricsPrometheusURL = %q, want the Prometheus the API pod reaches", got)
	}
}

func TestTheStatusMetricsFallBackToTheWorkloadPrometheus(t *testing.T) {
	cfg := config.Config{PrometheusAddress: "http://prometheus.example.test"}

	if got := metricsPrometheusURL(cfg); got != "http://prometheus.example.test" {
		t.Errorf("metricsPrometheusURL = %q, want LOOPRUNTIME_PROMETHEUS_ADDRESS when nothing else is set", got)
	}
}
