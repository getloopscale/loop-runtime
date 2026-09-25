// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Lock contention metrics — emitted by withEnvironmentLock.
var (
	LockAcquired = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_lock_acquired_total",
		Help: "Number of environment lock acquisitions.",
	}, []string{"tenant", "environment"})

	LockConflicts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_lock_conflicts_total",
		Help: "Number of environment lock acquisition failures (409 conflict).",
	}, []string{"tenant", "environment"})

	LockDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_lock_hold_duration_seconds",
		Help:    "Duration the environment lock was held.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30},
	}, []string{"tenant", "environment"})
)
