package projectmetricshttp

import (
	"strings"
	"testing"
)

// loopzero#170 regression: the error_rate signal must be a PERCENTAGE (ratio ×100),
// not a raw 5xx rate. The dashboard SignalGrid formats error_rate with
// fmtPercent ("X%"); a raw req/s rate formatted as "%" is a wrong, misleading
// number. The canonical query is sum(rate(5xx)) / sum(rate(total)) * 100.
func TestBuildPromQL_ErrorRate_IsRatioPercent(t *testing.T) {
	q := buildPromQL("error_rate", "tn-1", "app")

	if !strings.Contains(q, "* 100") {
		t.Errorf("error_rate must be a percentage (×100), got: %s", q)
	}
	if !strings.Contains(q, `status=~"5.."`) {
		t.Errorf("error_rate numerator must filter 5xx, got: %s", q)
	}
	// Ratio = 5xx over TOTAL requests → http_requests_total referenced twice
	// (numerator with the 5xx filter, denominator without).
	if strings.Count(q, "http_requests_total") < 2 {
		t.Errorf("error_rate must divide 5xx by total requests, got: %s", q)
	}
	// Tenant + project labels must still be injected server-side.
	if !strings.Contains(q, `tenant_id="tn-1"`) || !strings.Contains(q, `project="app"`) {
		t.Errorf("error_rate must carry tenant+project labels, got: %s", q)
	}
}

// Regression guard: the OTHER signals keep their canonical queries unchanged.
func TestBuildPromQL_OtherSignalsIntact(t *testing.T) {
	cases := map[string]string{
		"uptime_24h":     "avg_over_time(up{",
		"p95_latency":    "histogram_quantile(0.95",
		"rps":            "rate(http_requests_total{",
		"replicas_ready": "kube_deployment_status_replicas_ready{",
	}
	for signal, want := range cases {
		if q := buildPromQL(signal, "t", "p"); !strings.Contains(q, want) {
			t.Errorf("%s query changed — want substring %q, got: %s", signal, want, q)
		}
	}
	// Unknown signal falls through to a safe up{} query.
	if q := buildPromQL("bogus", "t", "p"); !strings.Contains(q, "up{") {
		t.Errorf("unknown signal must fall through to up{}, got: %s", q)
	}
}
