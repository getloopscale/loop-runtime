// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	prommodel "github.com/prometheus/common/model"
)

// T1.9 production wiring — HTTP-backed PromQuerier for cloud control plane.
//
// HTTPPromQuerier is the production implementation of PromQuerier (defined
// in cost_aggregator.go). Wraps the prometheus/client_golang API client and
// extracts tenant→value pairs from PromQL Vector results.

// HTTPPromQuerier queries a Prometheus HTTP API endpoint.
type HTTPPromQuerier struct {
	api    promv1.API
	logger *slog.Logger
	// queryTimeout bounds the wait on a single PromQL query so a slow
	// Prometheus instance cannot stall the entire aggregator tick.
	queryTimeout time.Duration
}

// NewHTTPPromQuerier constructs a querier pointed at the given Prometheus
// HTTP base URL (e.g., "http://prometheus.loopzero-system.svc.cluster.local:9090").
//
// Returns error if the URL is malformed. The HTTP client uses the default
// transport — no auth headers, suitable for in-cluster Prometheus served
// without authentication. For external Prometheus, wrap with a custom
// http.RoundTripper before constructing.
func NewHTTPPromQuerier(prometheusURL string, logger *slog.Logger) (*HTTPPromQuerier, error) {
	if prometheusURL == "" {
		return nil, errors.New("prometheus URL is empty")
	}
	if logger == nil {
		logger = slog.Default()
	}
	client, err := promapi.NewClient(promapi.Config{Address: prometheusURL})
	if err != nil {
		return nil, fmt.Errorf("prometheus client: %w", err)
	}
	return &HTTPPromQuerier{
		api:          promv1.NewAPI(client),
		logger:       logger,
		queryTimeout: 10 * time.Second,
	}, nil
}

// WithQueryTimeout sets the per-query timeout. Returns the receiver for
// chaining. Use when Prometheus is colocated and 1s is enough, or when
// it is across a slow link.
func (q *HTTPPromQuerier) WithQueryTimeout(d time.Duration) *HTTPPromQuerier {
	q.queryTimeout = d
	return q
}

// QueryCounterByTenant implements PromQuerier.
//
// Executes the PromQL query at "now" and parses the result as a Vector
// (one sample per tenant). Returns map[tenantID]value where tenantID is
// read from the `tenant` label.
//
// Behavior on edge cases:
//   - Query returns warnings → logged, results still returned.
//   - Result is not a Vector (e.g., Scalar or Matrix) → returns empty
//     map without error (caller treats as "no data this tick").
//   - Sample lacks `tenant` label → skipped silently (no panic).
//   - HTTP error → returned to caller. CostAggregator.Tick logs and
//     skips the metric (fail-open).
func (q *HTTPPromQuerier) QueryCounterByTenant(ctx context.Context, query string) (map[string]float64, error) {
	if q == nil || q.api == nil {
		return nil, errors.New("nil HTTPPromQuerier")
	}

	queryCtx, cancel := context.WithTimeout(ctx, q.queryTimeout)
	defer cancel()

	result, warnings, err := q.api.Query(queryCtx, query, time.Now())
	if err != nil {
		return nil, fmt.Errorf("promql query: %w", err)
	}
	if len(warnings) > 0 {
		q.logger.Warn("prometheus query returned warnings",
			"query", query,
			"warnings", warnings)
	}

	vector, ok := result.(prommodel.Vector)
	if !ok {
		// Scalar/Matrix/String results are not what aggregator expects.
		// Empty map = no upserts this tick. Caller may log via debug.
		q.logger.Debug("prometheus query did not return a Vector; skipping tick for this metric",
			"query", query,
			"result_type", fmt.Sprintf("%T", result))
		return map[string]float64{}, nil
	}

	out := make(map[string]float64, len(vector))
	for _, sample := range vector {
		tenant, ok := sample.Metric["tenant"]
		if !ok || tenant == "" {
			// Missing tenant label — skip. Could be aggregation rule
			// drift. Visible via prometheus_query_skipped metric (TODO).
			continue
		}
		out[string(tenant)] = float64(sample.Value)
	}
	return out, nil
}
