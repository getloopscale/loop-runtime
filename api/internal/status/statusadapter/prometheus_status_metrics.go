// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statusadapter

import (
	"context"
	"fmt"
	"math"
	"time"

	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"github.com/getloopscale/loopzero/api/internal/status/statushttp"
)

// The series each cell's Prometheus records per tenant app and the core
// Prometheus federates (infra/helm/monitoring/values-tenant-app-recording-rules.yaml).
// Each carries tenant_id, project, app and environment.
const (
	appReadyReplicasSeries      = "loopzero_app:kube_pod_status_ready:sum"
	appRequestRateSeries        = "loopzero_app:traefik_service_requests:rate5m"
	appRequestRateByClassSeries = "loopzero_app_status_class:traefik_service_requests:rate5m"
)

// PrometheusStatusMetrics reads the live numbers of the status answer with one
// instant Prometheus query each.
type PrometheusStatusMetrics struct {
	api promv1.API
}

var _ statushttp.StatusMetricsSource = (*PrometheusStatusMetrics)(nil)

// NewPrometheusStatusMetrics returns a source querying the Prometheus server
// at address.
func NewPrometheusStatusMetrics(address string) (*PrometheusStatusMetrics, error) {
	client, err := promapi.NewClient(promapi.Config{Address: address})
	if err != nil {
		return nil, fmt.Errorf("prometheus client for %q: %w", address, err)
	}
	return &PrometheusStatusMetrics{api: promv1.NewAPI(client)}, nil
}

// environmentMatcher selects the series of one environment of a project. The
// values come from the session and the resolved project, never from the URL
// alone, and %q escapes them for PromQL.
func environmentMatcher(tenantID, project, environment string) string {
	return fmt.Sprintf("tenant_id=%q,project=%q,environment=%q", tenantID, project, environment)
}

// ReadyReplicas implements statushttp.StatusMetricsSource. The sum folds the
// environment's apps, and the cells that serve it, into one count.
func (s *PrometheusStatusMetrics) ReadyReplicas(ctx context.Context, tenantID, project, environment string) (*int, error) {
	query := fmt.Sprintf("sum(%s{%s})", appReadyReplicasSeries, environmentMatcher(tenantID, project, environment))
	value, err := s.scalar(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query ready replicas: %w", err)
	}
	if value == nil || *value < 0 {
		return nil, nil
	}
	ready := int(min(*value, math.MaxInt32))
	return &ready, nil
}

// ServerErrorPercent implements statushttp.StatusMetricsSource. An app that
// answered no 5xx has no 5xx series, so the numerator falls back to 0 and the
// share reads 0. Without requests the division has no finite result, which is
// reported as no value.
func (s *PrometheusStatusMetrics) ServerErrorPercent(ctx context.Context, tenantID, project, environment string, window time.Duration) (*float64, error) {
	matcher := environmentMatcher(tenantID, project, environment)
	over := model.Duration(window).String()
	query := fmt.Sprintf(
		`(sum(avg_over_time(%s{status_class="5xx",%s}[%s])) or vector(0)) / sum(avg_over_time(%s{%s}[%s])) * 100`,
		appRequestRateByClassSeries, matcher, over, appRequestRateSeries, matcher, over)
	value, err := s.scalar(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query 5xx share: %w", err)
	}
	if value == nil || *value < 0 {
		return nil, nil
	}
	return value, nil
}

// scalar runs an instant query that aggregates to at most one sample. It
// returns nil when the result is empty or not finite.
func (s *PrometheusStatusMetrics) scalar(ctx context.Context, query string) (*float64, error) {
	value, _, err := s.api.Query(ctx, query, time.Now())
	if err != nil {
		return nil, err
	}
	vector, ok := value.(model.Vector)
	if !ok {
		return nil, fmt.Errorf("result is %s, want a vector", value.Type())
	}
	if len(vector) == 0 {
		return nil, nil
	}
	sample := float64(vector[0].Value)
	if math.IsNaN(sample) || math.IsInf(sample, 0) {
		return nil, nil
	}
	return &sample, nil
}
