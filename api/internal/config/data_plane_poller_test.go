// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad_DataPlanePollerIsOffWithAThirtySecondIntervalByDefault(t *testing.T) {
	t.Setenv("LOOPZERO_DATA_PLANE_POLLER_ENABLED", "")
	t.Setenv("LOOPZERO_DATA_PLANE_POLLER_INTERVAL", "")

	cfg := Load()

	if cfg.DataPlanePollerEnabled || cfg.DataPlanePollerInterval != 30*time.Second {
		t.Fatalf("enabled=%v interval=%s, want off and 30s", cfg.DataPlanePollerEnabled, cfg.DataPlanePollerInterval)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate with the poller off: %v", err)
	}
}

func TestValidate_AcceptsAnEnabledPollerWithAPrometheusURL(t *testing.T) {
	t.Setenv("LOOPZERO_DATA_PLANE_POLLER_ENABLED", "true")
	t.Setenv("LOOPZERO_DATA_PLANE_POLLER_PROMETHEUS_URL", "http://prometheus-server.monitoring.svc.cluster.local:80")
	t.Setenv("LOOPZERO_DATA_PLANE_POLLER_INTERVAL", "15s")

	cfg := Load()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.DataPlanePollerInterval != 15*time.Second {
		t.Errorf("interval = %s, want 15s", cfg.DataPlanePollerInterval)
	}
}

func TestValidate_RefusesAnEnabledPollerWithoutAPrometheusURL(t *testing.T) {
	t.Setenv("LOOPZERO_DATA_PLANE_POLLER_ENABLED", "true")
	t.Setenv("LOOPZERO_DATA_PLANE_POLLER_PROMETHEUS_URL", "")

	err := Load().Validate()

	if err == nil || !strings.Contains(err.Error(), "LOOPZERO_DATA_PLANE_POLLER_PROMETHEUS_URL") {
		t.Fatalf("Validate err = %v, want it to name LOOPZERO_DATA_PLANE_POLLER_PROMETHEUS_URL", err)
	}
}

func TestValidate_RefusesAPrometheusURLThatIsNotAbsoluteHTTP(t *testing.T) {
	t.Setenv("LOOPZERO_DATA_PLANE_POLLER_ENABLED", "true")
	t.Setenv("LOOPZERO_DATA_PLANE_POLLER_PROMETHEUS_URL", "prometheus-server:80")

	err := Load().Validate()

	if err == nil || !strings.Contains(err.Error(), "LOOPZERO_DATA_PLANE_POLLER_PROMETHEUS_URL") {
		t.Fatalf("Validate err = %v, want the URL refused", err)
	}
}

func TestValidate_RefusesANonPositivePollerInterval(t *testing.T) {
	t.Setenv("LOOPZERO_DATA_PLANE_POLLER_ENABLED", "true")
	t.Setenv("LOOPZERO_DATA_PLANE_POLLER_PROMETHEUS_URL", "http://prometheus:9090")
	t.Setenv("LOOPZERO_DATA_PLANE_POLLER_INTERVAL", "0s")

	err := Load().Validate()

	if err == nil || !strings.Contains(err.Error(), "LOOPZERO_DATA_PLANE_POLLER_INTERVAL") {
		t.Fatalf("Validate err = %v, want it to name LOOPZERO_DATA_PLANE_POLLER_INTERVAL", err)
	}
}

func TestValidate_RefusesAMalformedPollerInterval(t *testing.T) {
	t.Setenv("LOOPZERO_DATA_PLANE_POLLER_INTERVAL", "30")

	err := Load().Validate()

	if err == nil || !strings.Contains(err.Error(), "LOOPZERO_DATA_PLANE_POLLER_INTERVAL") {
		t.Fatalf("Validate err = %v, want the unit-less interval refused", err)
	}
}
