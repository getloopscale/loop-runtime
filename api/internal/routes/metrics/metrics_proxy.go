// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package metrics serves /api/v1/projects/{name}/metrics: the proxy that answers
// a project's metrics query, and the Prometheus client behind it.
//
// Twenty-third family to leave the flat `routes` package (B-080, slice-40). The
// proxy and the client ship together because they are one read path, even though
// the proxy does not name the client's type — the wiring happens in cmd.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/api/internal/httpio"
	"github.com/getloopscale/loopzero/api/internal/ports"

	"github.com/go-chi/chi/v5"

	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// T1.2 of plan 2026-06-06-dashboard-journey-j18-status-runtime-health v1.1.
//
// MetricsProxyHandler proxies Prometheus `query_range` for J-18 SignalDetailSheet
// histogram rendering. EC-2 absorption: tenant + project labels are injected
// server-side from the authenticated session context — the URL is NEVER trusted
// for tenant scoping. Cross-tenant project access returns 404 (NOT 403) per
// existence-leak-prevention pattern (per memory feedback on tenant isolation).

// PrometheusDataPoint is a single (timestamp, value) datapoint from a Prometheus
// query_range result series. Timestamp is Unix seconds; value is the metric scalar.
type PrometheusDataPoint struct {
	Timestamp int64   `json:"t"`
	Value     float64 `json:"v"`
}

// PrometheusQueryClient abstracts the Prometheus HTTP API for testing (DIP).
// Hermetic Go tests inject a fake implementation; production uses a real
// HTTP client targeting cfg.PrometheusAddress.
type PrometheusQueryClient interface {
	// QueryRange executes a Prometheus query_range API call.
	// Returns context.DeadlineExceeded on timeout (mapped to HTTP 504).
	// Returns any other error mapped to HTTP 502.
	// Empty result (no datapoints) is NOT an error — handler maps to
	// `{ status: "collecting" }` per EC-8 absorption (project < 24h old).
	QueryRange(ctx context.Context, promQL string, startUnix, endUnix int64, stepSeconds int) ([]PrometheusDataPoint, error)
}

// StatusProjectStore resolves a project by name for the caller's tenant.
//
// Declared here rather than imported from the status family that also declares
// it: a Go interface is structural, so the consumer naming exactly the method it
// calls is both idiomatic and the DIP shape rules/architecture.md asks for. The
// fourth package to make this call in the routes decomposition — the dependency
// is on ONE method, not on a package (B-080, slice-39).
type StatusProjectStore interface {
	GetByName(ctx context.Context, tenantID, name string) (*ports.ProjectResult, error)
}

// MetricsProxyHandler handles GET /api/v1/projects/{name}/metrics?range=7d&signal=uptime
type MetricsProxyHandler struct {
	projects   StatusProjectStore
	promClient PrometheusQueryClient
}

// NewMetricsProxyHandler creates a new MetricsProxyHandler.
//
// promClient may be nil to disable the handler (Get returns 503). Production
// wires a real Prometheus HTTP client; tests inject httptest-backed fakes.
func NewMetricsProxyHandler(projects StatusProjectStore, promClient PrometheusQueryClient) *MetricsProxyHandler {
	return &MetricsProxyHandler{
		projects:   projects,
		promClient: promClient,
	}
}

// metricsRangeResponse is the JSON wire response for GET /metrics.
type metricsRangeResponse struct {
	Signal         string                `json:"signal"`
	Range          string                `json:"range"`
	Status         string                `json:"status"` // "ok" | "collecting"
	Data           []PrometheusDataPoint `json:"data"`
	CollectingHint string                `json:"collecting_hint,omitempty"`
}

// Get handles GET /api/v1/projects/{name}/metrics?range=7d&signal=uptime_24h.
//
// Authentication: requires session ctx via auth.GetUserContext.
// Authorization: project ownership verified via projects.GetByName(uc.TenantID, name).
//
//	Cross-tenant returns 404 NOT_FOUND (existence-leak-prevention).
//
// Prometheus call: 5s timeout per EC-5; timeout → HTTP 504; error → HTTP 502.
// Empty result → 200 OK with `{ status: "collecting", data: [] }` per EC-8.
func (h *MetricsProxyHandler) Get(w http.ResponseWriter, r *http.Request) {
	uc := auth.GetUserContext(r.Context())
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project name is required"))
		return
	}

	if h.promClient == nil {
		httpio.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":       "prometheus_not_configured",
			"retry_after": "30",
		})
		return
	}

	// EC-2 absorption: project ownership check. Cross-tenant project →
	// 404 (existence-leak-prevention), NOT 403. Same pattern as
	// status.go:191.
	project, err := h.projects.GetByName(r.Context(), uc.TenantID, name)
	if err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if project == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
		return
	}

	signal := r.URL.Query().Get("signal")
	if signal == "" {
		signal = "uptime_24h"
	}
	rangeStr := r.URL.Query().Get("range")
	if rangeStr == "" {
		rangeStr = "7d"
	}

	// EC-2 absorption: PromQL is built server-side with tenant_id + project
	// labels derived from session ctx, NOT from URL. The signal parameter
	// chooses the query template; the labels are mandatory.
	promQL := buildPromQL(signal, uc.TenantID, name)

	end := time.Now().Unix()
	rangeDur := parseMetricsRange(rangeStr)
	start := end - int64(rangeDur.Seconds())
	step := int(rangeDur.Seconds()) / 200
	if step < 60 {
		step = 60
	}

	// EC-5 absorption: 5s timeout. Prometheus down → 504 honest
	// (NOT silent 0 — UI consumer renders "—" not "0%").
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	data, err := h.promClient.QueryRange(ctx, promQL, start, end, step)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			httpio.WriteJSON(w, http.StatusGatewayTimeout, map[string]string{
				"error":       "prometheus_unreachable",
				"retry_after": "30",
			})
			return
		}
		httpio.WriteJSON(w, http.StatusBadGateway, map[string]string{
			"error": "prometheus_invalid_response",
		})
		return
	}

	resp := metricsRangeResponse{
		Signal: signal,
		Range:  rangeStr,
		Data:   data,
	}

	// EC-8 absorption: empty histogram → collecting state with hint.
	// The hint is product copy, not an API message: the dashboard
	// (useMetricsRange + SignalDetailSheet) renders it verbatim in place of an
	// empty chart, in the dashboard's own UI language, and falls back to the
	// same text when the field is absent. Clients key on Status, never on it.
	if len(data) == 0 {
		resp.Status = "collecting"
		resp.CollectingHint = "Coletando dados..." // english-only: dashboard UI copy, rendered verbatim in the product's Portuguese UI
	} else {
		resp.Status = "ok"
	}

	httpio.WriteJSON(w, http.StatusOK, resp)
}

// buildPromQL injects mandatory tenant_id + project labels into the Prometheus
// query template for the requested signal. EC-2 absorption: labels come from
// session ctx, NOT URL. Defense against cross-tenant data leak.
//
// Unknown signal names fall through to a safe `up{...}` query — the UI never
// triggers this path (consumers use PromQueryKeys from statusResponse), but
// any caller bypassing it gets a constrained well-formed query, not panic.
func buildPromQL(signal, tenantID, projectName string) string {
	labels := fmt.Sprintf(`tenant_id=%q,project=%q`, tenantID, projectName)
	switch signal {
	case "uptime_24h":
		return fmt.Sprintf("avg_over_time(up{%s}[24h])", labels)
	case "replicas_ready":
		return fmt.Sprintf("kube_deployment_status_replicas_ready{%s}", labels)
	case "p95_latency":
		return fmt.Sprintf("histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{%s}[5m]))", labels)
	case "error_rate":
		// loopzero#170: error_rate is displayed as a PERCENTAGE by the dashboard
		// (SignalGrid fmtPercent). It must be the ratio of 5xx to total requests
		// ×100, NOT a raw 5xx req/s rate. Division by zero (no traffic) yields an
		// empty/NaN series → the handler maps that to "collecting".
		return fmt.Sprintf(`sum(rate(http_requests_total{status=~"5..",%s}[5m])) / sum(rate(http_requests_total{%s}[5m])) * 100`, labels, labels)
	case "rps":
		return fmt.Sprintf("rate(http_requests_total{%s}[1m])", labels)
	case "status_terminal":
		return fmt.Sprintf("loopzero_release_status_terminal{%s}", labels)
	case "alerts_active":
		return fmt.Sprintf("ALERTS{alertstate=%q,%s}", "firing", labels)
	case "last_deploy_time":
		return fmt.Sprintf("loopzero_release_last_deploy_timestamp_seconds{%s}", labels)
	default:
		return fmt.Sprintf("up{%s}", labels)
	}
}

// parseMetricsRange parses the human-friendly range string used by the URL
// (7d / 24h / 1h) into a time.Duration. Unknown values default to 7d.
func parseMetricsRange(s string) time.Duration {
	switch strings.ToLower(s) {
	case "7d":
		return 7 * 24 * time.Hour
	case "24h":
		return 24 * time.Hour
	case "1h":
		return 1 * time.Hour
	case "30m":
		return 30 * time.Minute
	default:
		return 7 * 24 * time.Hour
	}
}
