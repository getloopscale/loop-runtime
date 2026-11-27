package projectmetricshttp_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/api/internal/projectmetrics/projectmetricshttp"
)

// A project has one series per environment. Without the environment in the query,
// production with 2 replicas and preview with 2 read as 4 ready replicas, and the
// traffic of every environment was summed into one number.
func TestMetricsProxy_ScopesTheAppSignalsToOneEnvironment(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, query, want string
	}{
		{"the environment asked for", "signal=replicas_ready&range=1h&env=preview", `environment="preview"`},
		{"production when none is named", "signal=replicas_ready&range=1h", `environment="production"`},
		{"request rate", "signal=rps&range=1h&env=qa-env", `environment="qa-env"`},
		{"latency", "signal=p95_latency&range=1h&env=preview", `environment="preview"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			promClient := &fakePrometheusQueryClient{
				data: []projectmetricshttp.PrometheusDataPoint{{Timestamp: 1000, Value: 2}},
			}
			handler := projectmetricshttp.NewMetricsProxyHandler(
				&fakeProjectStore{project: &ports.ProjectResult{ID: "proj-1", Name: "my-app", TeamID: "acme"}}, promClient)
			rec := httptest.NewRecorder()

			handler.Get(rec, newMetricsRequest("my-app", tc.query, true))

			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(promClient.lastPromQL, tc.want) {
				t.Errorf("the query does not carry %s: %s", tc.want, promClient.lastPromQL)
			}
		})
	}
}

// The error rate divides two sums. Both sides must read the same environment, or the
// 5xx of one environment are divided by the traffic of another.
func TestMetricsProxy_ErrorRateReadsOneEnvironmentOnBothSides(t *testing.T) {
	t.Parallel()

	promClient := &fakePrometheusQueryClient{
		data: []projectmetricshttp.PrometheusDataPoint{{Timestamp: 1000, Value: 0}},
	}
	handler := projectmetricshttp.NewMetricsProxyHandler(
		&fakeProjectStore{project: &ports.ProjectResult{ID: "proj-1", Name: "my-app", TeamID: "acme"}}, promClient)
	rec := httptest.NewRecorder()

	handler.Get(rec, newMetricsRequest("my-app", "signal=error_rate&range=1h&env=preview", true))

	if got := strings.Count(promClient.lastPromQL, `environment="preview"`); got != 2 {
		t.Errorf("the query names the environment %d times, want 2: %s", got, promClient.lastPromQL)
	}
}

// The environment reaches a label matcher, so a value that is not an environment name is
// refused before any query is built.
func TestMetricsProxy_RefusesAnEnvironmentThatIsNotAName(t *testing.T) {
	t.Parallel()

	promClient := &fakePrometheusQueryClient{}
	handler := projectmetricshttp.NewMetricsProxyHandler(
		&fakeProjectStore{project: &ports.ProjectResult{ID: "proj-1", Name: "my-app", TeamID: "acme"}}, promClient)
	rec := httptest.NewRecorder()

	handler.Get(rec, newMetricsRequest("my-app", `signal=rps&range=1h&env=preview%22%7D%20or%20up%7B`, true))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if promClient.lastPromQL != "" {
		t.Errorf("Prometheus was queried with %s", promClient.lastPromQL)
	}
}
