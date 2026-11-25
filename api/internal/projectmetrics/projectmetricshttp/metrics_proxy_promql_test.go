package projectmetricshttp

import (
	"strings"
	"testing"
)

// The Status signals read the series each cell records per tenant app
// (infra/helm/monitoring/values-tenant-app-recording-rules.yaml) and the core
// Prometheus federates. A tenant app is an Argo Rollout that LoopScale does not
// instrument, so kube_deployment_status_replicas_ready and http_requests_total
// hold nothing for it (getloopscale/loopzero#107).
func TestBuildPromQL_ReadsTheSeriesRecordedPerTenantApp(t *testing.T) {
	const labels = `tenant_id="tn-1",project="shop"`
	cases := map[string]string{
		"replicas_ready": `sum(loopzero_app:kube_pod_status_ready:sum{` + labels + `})`,
		"p95_latency":    `histogram_quantile(0.95, sum by (le) (loopzero_app_le:traefik_service_request_duration_seconds_bucket:rate5m{` + labels + `}))`,
		"rps":            `sum(loopzero_app:traefik_service_requests:rate5m{` + labels + `})`,
		"error_rate": `(sum(loopzero_app_status_class:traefik_service_requests:rate5m{status_class="5xx",` + labels + `}) or vector(0))` +
			` / sum(loopzero_app:traefik_service_requests:rate5m{` + labels + `}) * 100`,
	}
	for signal, want := range cases {
		if got := buildPromQL(signal, "tn-1", "shop"); got != want {
			t.Errorf("%s:\n got  %s\n want %s", signal, got, want)
		}
	}
}

// The client reads the first series of a result (prometheus_client.go), so a
// project with several apps, environments or cells must come back as one.
func TestBuildPromQL_TenantAppSignalsAggregateToOneSeries(t *testing.T) {
	for _, signal := range []string{"replicas_ready", "rps"} {
		if q := buildPromQL(signal, "t", "p"); !strings.HasPrefix(q, "sum(") {
			t.Errorf("%s is not aggregated to one series: %s", signal, q)
		}
	}
	if q := buildPromQL("p95_latency", "t", "p"); !strings.Contains(q, "sum by (le) (") {
		t.Errorf("p95_latency does not merge the buckets of every app by le: %s", q)
	}
}

// loopzero#170: the dashboard formats error_rate as a percentage, so it is the
// ratio of 5xx to all requests times 100. An app that served traffic and no 5xx
// has no 5xx series at all; it reads 0, not an empty result.
func TestBuildPromQL_ErrorRate_IsAPercentageThatReadsZeroWithoutErrors(t *testing.T) {
	q := buildPromQL("error_rate", "tn-1", "app")

	if !strings.HasSuffix(q, "* 100") {
		t.Errorf("error_rate must be a percentage (ratio times 100), got: %s", q)
	}
	if !strings.Contains(q, `status_class="5xx"`) {
		t.Errorf("error_rate numerator must select the 5xx class, got: %s", q)
	}
	if !strings.Contains(q, "or vector(0)) / ") {
		t.Errorf("error_rate numerator must fall back to 0 when no 5xx series exists, got: %s", q)
	}
	if strings.Count(q, `tenant_id="tn-1",project="app"`) != 2 {
		t.Errorf("numerator and denominator must both carry the tenant and project labels, got: %s", q)
	}
}

// Label values are quoted, so a value cannot close the selector and widen it.
func TestBuildPromQL_QuotesLabelValues(t *testing.T) {
	q := buildPromQL("rps", `tn-1"}`, `shop",tenant_id=~".+`)
	if !strings.Contains(q, `tenant_id="tn-1\"}"`) || !strings.Contains(q, `project="shop\",tenant_id=~\".+"`) {
		t.Errorf("label values are not escaped: %s", q)
	}
}

// The signals this change does not touch keep their queries.
func TestBuildPromQL_OtherSignalsIntact(t *testing.T) {
	cases := map[string]string{
		"uptime_24h":       "avg_over_time(up{",
		"status_terminal":  "loopzero_release_status_terminal{",
		"last_deploy_time": "loopzero_release_last_deploy_timestamp_seconds{",
	}
	for signal, want := range cases {
		if q := buildPromQL(signal, "t", "p"); !strings.Contains(q, want) {
			t.Errorf("%s query changed: want substring %q, got: %s", signal, want, q)
		}
	}
	// Unknown signal falls through to a safe up{} query.
	if q := buildPromQL("bogus", "t", "p"); !strings.Contains(q, "up{") {
		t.Errorf("unknown signal must fall through to up{}, got: %s", q)
	}
}
