// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"context"
	"encoding/json"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/routes/metrics"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// T1.2 of plan 2026-06-06-dashboard-journey-j18-status-runtime-health v1.1.
//
// Tests metricsProxyHandler with hermetic Prometheus mocks. Covers:
//  - EC-2 absorption: cross-tenant project → 404 NOT_FOUND
//  - EC-2 absorption: PromQL injects tenant_id + project labels server-side
//  - EC-5 absorption: Prometheus timeout → 504 honest
//  - EC-8 absorption: empty histogram → 200 OK with collecting status
//  - Happy path: data → 200 OK with status=ok
//  - Edge: nil promClient → 503 prometheus_not_configured
//  - Edge: auth missing → 401

// fakePrometheusQueryClient implements routes.PrometheusQueryClient for testing.
type fakePrometheusQueryClient struct {
	data       []metrics.PrometheusDataPoint
	err        error
	lastPromQL string // captures last PromQL invocation for label-injection assertion
}

func (f *fakePrometheusQueryClient) QueryRange(ctx context.Context, promQL string, startUnix, endUnix int64, stepSeconds int) ([]metrics.PrometheusDataPoint, error) {
	f.lastPromQL = promQL
	if f.err != nil {
		return nil, f.err
	}
	return f.data, nil
}

// newMetricsRequest creates a GET request with chi URL params + auth ctx.
func newMetricsRequest(projectName, query string, authenticated bool) *http.Request {
	url := "/api/v1/projects/" + projectName + "/metrics"
	if query != "" {
		url += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", projectName)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	if authenticated {
		req = req.WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rctx2 := chi.NewRouteContext()
		rctx2.URLParams.Add("name", projectName)
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx2)
		req = req.WithContext(ctx)
	}
	return req
}

func TestMetricsProxy_Get(t *testing.T) {
	t.Parallel()

	t.Run("cross-tenant project returns 404 NOT_FOUND (existence-leak-prevention)", func(t *testing.T) {
		// Arrange: project store returns nil (project doesn't belong to tenant
		// "acme") simulating cross-tenant lookup. Per existence-leak-prevention,
		// handler must return 404 NOT 403.
		projects := &fakeStatusProjectStore{project: nil}
		promClient := &fakePrometheusQueryClient{}
		handler := metrics.NewMetricsProxyHandler(projects, promClient)
		req := newMetricsRequest("other-tenant-project", "signal=uptime_24h&range=7d", true)
		rec := httptest.NewRecorder()

		// Act
		handler.Get(rec, req)

		// Assert
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404 NotFound for cross-tenant project, got %d: %s", rec.Code, rec.Body.String())
		}
		// Defense: Prometheus must NOT be queried for cross-tenant projects.
		if promClient.lastPromQL != "" {
			t.Errorf("Prometheus must not be queried on cross-tenant 404; lastPromQL=%q", promClient.lastPromQL)
		}
	})

	t.Run("PromQL injects tenant_id and project labels server-side", func(t *testing.T) {
		// Arrange: project owned by acme tenant; handler must build PromQL
		// with `tenant_id="acme"` and `project="my-app"` labels — derived from
		// session ctx (makeAuthCtx tenant=acme), NOT from URL.
		projects := &fakeStatusProjectStore{
			project: &ports.ProjectResult{ID: "proj-1", Name: "my-app", TeamID: "acme"},
		}
		promClient := &fakePrometheusQueryClient{
			data: []metrics.PrometheusDataPoint{{Timestamp: 1000, Value: 0.99}},
		}
		handler := metrics.NewMetricsProxyHandler(projects, promClient)
		req := newMetricsRequest("my-app", "signal=uptime_24h&range=7d", true)
		rec := httptest.NewRecorder()

		// Act
		handler.Get(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(promClient.lastPromQL, `tenant_id="acme"`) {
			t.Errorf("PromQL must inject tenant_id=acme label; got: %q", promClient.lastPromQL)
		}
		if !strings.Contains(promClient.lastPromQL, `project="my-app"`) {
			t.Errorf("PromQL must inject project=my-app label; got: %q", promClient.lastPromQL)
		}
	})

	t.Run("Prometheus timeout returns 504 prometheus_unreachable honest", func(t *testing.T) {
		// Arrange: Prometheus client returns context.DeadlineExceeded
		// simulating 5s timeout. Handler must return HTTP 504 NOT silent 0.
		projects := &fakeStatusProjectStore{
			project: &ports.ProjectResult{ID: "proj-1", Name: "my-app", TeamID: "acme"},
		}
		promClient := &fakePrometheusQueryClient{err: context.DeadlineExceeded}
		handler := metrics.NewMetricsProxyHandler(projects, promClient)
		req := newMetricsRequest("my-app", "signal=uptime_24h&range=7d", true)
		rec := httptest.NewRecorder()

		// Act
		handler.Get(rec, req)

		// Assert
		if rec.Code != http.StatusGatewayTimeout {
			t.Fatalf("expected 504 on timeout, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["error"] != "prometheus_unreachable" {
			t.Errorf("expected error=prometheus_unreachable (honest), got %q", resp["error"])
		}
		if resp["retry_after"] == "" {
			t.Error("expected retry_after to be set on 504")
		}
	})

	t.Run("empty histogram returns 200 with collecting status", func(t *testing.T) {
		// Arrange: project < 24h old simulated by Prometheus returning empty
		// result. Handler must return 200 OK with status=collecting + hint —
		// UI consumer renders "Coletando dados..." NOT empty chart.
		projects := &fakeStatusProjectStore{
			project: &ports.ProjectResult{ID: "proj-1", Name: "my-app", TeamID: "acme"},
		}
		promClient := &fakePrometheusQueryClient{data: []metrics.PrometheusDataPoint{}}
		handler := metrics.NewMetricsProxyHandler(projects, promClient)
		req := newMetricsRequest("my-app", "signal=uptime_24h&range=7d", true)
		rec := httptest.NewRecorder()

		// Act
		handler.Get(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 on empty histogram (collecting state), got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["status"] != "collecting" {
			t.Errorf("expected status=collecting, got %q", resp["status"])
		}
		if resp["collecting_hint"] != "Coletando dados..." {
			t.Errorf("expected collecting_hint='Coletando dados...', got %q", resp["collecting_hint"])
		}
		// Data must be empty array (not null)
		if data, ok := resp["data"].([]interface{}); !ok || len(data) != 0 {
			t.Errorf("expected data=[] empty array, got %v", resp["data"])
		}
	})

	t.Run("Happy path: data returned with status=ok", func(t *testing.T) {
		// Arrange: Prometheus returns 3 datapoints.
		projects := &fakeStatusProjectStore{
			project: &ports.ProjectResult{ID: "proj-1", Name: "my-app", TeamID: "acme"},
		}
		promClient := &fakePrometheusQueryClient{
			data: []metrics.PrometheusDataPoint{
				{Timestamp: 1000, Value: 0.99},
				{Timestamp: 2000, Value: 0.98},
				{Timestamp: 3000, Value: 0.97},
			},
		}
		handler := metrics.NewMetricsProxyHandler(projects, promClient)
		req := newMetricsRequest("my-app", "signal=uptime_24h&range=7d", true)
		rec := httptest.NewRecorder()

		// Act
		handler.Get(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["status"] != "ok" {
			t.Errorf("expected status=ok, got %q", resp["status"])
		}
		if resp["signal"] != "uptime_24h" {
			t.Errorf("expected signal=uptime_24h, got %q", resp["signal"])
		}
		if resp["range"] != "7d" {
			t.Errorf("expected range=7d, got %q", resp["range"])
		}
		data, ok := resp["data"].([]interface{})
		if !ok {
			t.Fatalf("data field missing or not array, got %T", resp["data"])
		}
		if len(data) != 3 {
			t.Errorf("expected 3 datapoints, got %d", len(data))
		}
	})

	t.Run("Edge: nil promClient returns 503 prometheus_not_configured", func(t *testing.T) {
		// Arrange: handler constructed with nil Prometheus client (operator
		// did not configure LOOPZERO_PROMETHEUS_ADDRESS). Handler must surface
		// 503 honestly — NOT silently return empty data.
		projects := &fakeStatusProjectStore{
			project: &ports.ProjectResult{ID: "proj-1", Name: "my-app", TeamID: "acme"},
		}
		handler := metrics.NewMetricsProxyHandler(projects, nil)
		req := newMetricsRequest("my-app", "signal=uptime_24h&range=7d", true)
		rec := httptest.NewRecorder()

		// Act
		handler.Get(rec, req)

		// Assert
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503 when promClient is nil, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["error"] != "prometheus_not_configured" {
			t.Errorf("expected error=prometheus_not_configured, got %q", resp["error"])
		}
	})

	t.Run("Edge: missing auth returns 401", func(t *testing.T) {
		projects := &fakeStatusProjectStore{}
		promClient := &fakePrometheusQueryClient{}
		handler := metrics.NewMetricsProxyHandler(projects, promClient)
		req := newMetricsRequest("my-app", "signal=uptime_24h", false) // unauthenticated
		rec := httptest.NewRecorder()

		handler.Get(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 without auth, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Defaults: missing signal/range fall back to uptime_24h/7d", func(t *testing.T) {
		projects := &fakeStatusProjectStore{
			project: &ports.ProjectResult{ID: "proj-1", Name: "my-app", TeamID: "acme"},
		}
		promClient := &fakePrometheusQueryClient{
			data: []metrics.PrometheusDataPoint{{Timestamp: 1000, Value: 0.99}},
		}
		handler := metrics.NewMetricsProxyHandler(projects, promClient)
		req := newMetricsRequest("my-app", "", true) // no query params
		rec := httptest.NewRecorder()

		handler.Get(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp["signal"] != "uptime_24h" {
			t.Errorf("expected default signal=uptime_24h, got %q", resp["signal"])
		}
		if resp["range"] != "7d" {
			t.Errorf("expected default range=7d, got %q", resp["range"])
		}
	})
}
