// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// HTTP request metrics — emitted by the metrics middleware on every request.
var (
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopruntime_api_request_duration_seconds",
		Help:    "HTTP request duration in seconds.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"method", "path", "status"})

	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_api_requests_total",
		Help: "Total number of HTTP requests.",
	}, []string{"method", "path", "status"})

	HTTPRequestsInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopruntime_api_requests_in_flight",
		Help: "Number of HTTP requests currently being processed.",
	})
)

// Idempotency metrics — emitted by idempotency middleware.
var (
	IdempotencyHits = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_idempotency_cache_hits_total",
		Help: "Number of idempotency cache hits (deduplicated requests).",
	}, []string{"endpoint"})

	IdempotencyMisses = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_idempotency_cache_misses_total",
		Help: "Number of idempotency cache misses (new requests).",
	}, []string{"endpoint"})
)

// SSE stream metrics — emitted by SSE stream handler.
var (
	SSEConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopruntime_sse_connections_active",
		Help: "Number of active SSE streaming connections.",
	})

	SSEEventsEmitted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_sse_events_emitted_total",
		Help: "Total number of SSE events emitted to clients.",
	}, []string{"event_type"})
)
