// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ArgoCD circuit breaker + auth metrics (MEET-2026-03-28-013).
var (
	// ArgoCDCircuitState tracks the circuit breaker state.
	// 0=closed (normal), 1=open (rejecting), 2=half-open (probing).
	// Fixes phantom alert: LoopRuntimeArgoCDCircuitOpen referenced this metric but it was never emitted.
	ArgoCDCircuitState = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loop_runtime_argocd_circuit_state",
		Help: "ArgoCD circuit breaker state (0=closed, 1=open, 2=half-open).",
	})

	// ArgoCDAuthFailures counts ArgoCD authentication failures (401/403).
	// These are NOT counted toward the circuit breaker threshold.
	ArgoCDAuthFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loop_runtime_argocd_auth_failures_total",
		Help: "Total ArgoCD authentication failures (401/403). Indicates invalid or expired token.",
	})

	// ArgoCDTokenAgeSeconds is the age in seconds of the currently active
	// ArgoCD API token used by loop-runtime-api. -1 sentinel = unknown / no token
	// minted yet. Self-healing token provider exposes this so Prometheus
	// can alert on tokens that aren't being rotated as expected.
	// CHG-2026-04-30-DOGFOOD-LOCAL-HEAL NEW-15.
	ArgoCDTokenAgeSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loop_runtime_argocd_token_age_seconds",
		Help: "Seconds since the active ArgoCD API token was last minted by KubeMintingTokenProvider. -1 = unknown.",
	})

	// ArgoCDTokenRefreshesTotal counts token mint attempts by trigger and
	// outcome. trigger ∈ {startup, startup_empty, proactive, proactive_empty,
	// auth_failure, manual}. outcome ∈ {success, error, skipped}.
	ArgoCDTokenRefreshesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loop_runtime_argocd_token_refreshes_total",
		Help: "Total ArgoCD token mint attempts. Labels: trigger (proactive|auth_failure|...), outcome (success|error|skipped).",
	}, []string{"trigger", "outcome"})
)
