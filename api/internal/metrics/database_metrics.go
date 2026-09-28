// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Database connection pool metrics — emitted by a periodic collector.
var (
	DBConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_postgres_connections_active",
		Help: "Number of active PostgreSQL connections.",
	})

	DBConnectionsIdle = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_postgres_connections_idle",
		Help: "Number of idle PostgreSQL connections.",
	})

	DBConnectionsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_postgres_connections_total",
		Help: "Total number of PostgreSQL connections in the pool.",
	})

	// P2-9: Pool saturation metrics — detect contention before it causes failures.
	DBPoolMaxConns = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_postgres_pool_max_conns",
		Help: "Maximum configured connections in the pool.",
	})

	DBPoolAcquireCount = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_postgres_pool_acquire_total",
		Help: "Total number of connection acquire attempts.",
	})

	DBPoolAcquireDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "loopzero_postgres_pool_acquire_duration_seconds",
		Help:    "Time spent waiting for a connection from the pool.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 5.0},
	})

	DBPoolEmptyAcquireCount = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_postgres_pool_empty_acquire_total",
		Help: "Number of times all connections were in use when acquire was called (pool saturation signal).",
	})
)
