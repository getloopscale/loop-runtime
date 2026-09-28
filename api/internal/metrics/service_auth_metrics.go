// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Service auth secret age metric — WS.1 security blocker.
// Tracks how long the current service auth secret has been in use.
// Alert: LoopZeroServiceAuthSecretExpiring fires when age > 80 days.
var (
	ServiceAuthSecretAge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_service_auth_secret_age_seconds",
		Help: "Age of the current service auth secret in seconds.",
	})
)
