package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// J-18 T1.2 — the concrete PrometheusQueryClient. Proves the real HTTP
// query_range parse (matrix result → []PrometheusDataPoint, integer timestamps,
// float values), the outbound request shape, and the failure/empty semantics
// the MetricsProxyHandler relies on. No live Prometheus.
func TestPrometheusClient_QueryRange_ParsesMatrix(t *testing.T) {
	var gotQuery, gotStart, gotEnd, gotStep, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		q := r.URL.Query()
		gotQuery = q.Get("query")
		gotStart = q.Get("start")
		gotEnd = q.Get("end")
		gotStep = q.Get("step")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "matrix",
				"result": [
					{ "metric": {"__name__":"up"}, "values": [ [1751365200, "0.99"], [1751365260, "0.985"] ] }
				]
			}
		}`))
	}))
	defer srv.Close()

	c := NewPrometheusClient(srv.URL, nil)
	pts, err := c.QueryRange(context.Background(), `up{tenant_id="tn-1"}`, 1751365200, 1751365260, 60)
	if err != nil {
		t.Fatalf("QueryRange error: %v", err)
	}

	if gotPath != "/api/v1/query_range" {
		t.Errorf("path = %q, want /api/v1/query_range", gotPath)
	}
	if gotQuery != `up{tenant_id="tn-1"}` {
		t.Errorf("query = %q", gotQuery)
	}
	if gotStart != "1751365200" || gotEnd != "1751365260" || gotStep != "60" {
		t.Errorf("start/end/step = %q/%q/%q, want 1751365200/1751365260/60", gotStart, gotEnd, gotStep)
	}

	if len(pts) != 2 {
		t.Fatalf("got %d points, want 2", len(pts))
	}
	if pts[0].Timestamp != 1751365200 || pts[0].Value != 0.99 {
		t.Errorf("pt[0] = {%d, %v}, want {1751365200, 0.99}", pts[0].Timestamp, pts[0].Value)
	}
	if pts[1].Value != 0.985 {
		t.Errorf("pt[1].Value = %v, want 0.985", pts[1].Value)
	}
}

// Empty matrix (project < 24h old, no series yet) is NOT an error — the handler
// maps an empty slice to `{ status: "collecting" }`.
func TestPrometheusClient_QueryRange_EmptyIsNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer srv.Close()

	c := NewPrometheusClient(srv.URL, nil)
	pts, err := c.QueryRange(context.Background(), "up", 1, 2, 1)
	if err != nil {
		t.Fatalf("empty result must not error, got: %v", err)
	}
	if len(pts) != 0 {
		t.Errorf("got %d points, want 0", len(pts))
	}
}

// A non-200 Prometheus response is an error (handler maps to 502) — never a
// silent empty result. The error must not embed the query (tenant labels).
func TestPrometheusClient_QueryRange_Non200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"status":"error","error":"query for tn-secret failed"}`))
	}))
	defer srv.Close()

	c := NewPrometheusClient(srv.URL, nil)
	_, err := c.QueryRange(context.Background(), `up{tenant_id="tn-secret"}`, 1, 2, 1)
	if err == nil {
		t.Fatal("expected error on non-200")
	}
	if containsAnySub(err.Error(), "tn-secret") {
		t.Errorf("error leaks the query/tenant label: %q", err)
	}
}

// A cancelled context surfaces its error (handler maps to 504).
func TestPrometheusClient_QueryRange_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := NewPrometheusClient(srv.URL, nil)
	_, err := c.QueryRange(ctx, "up", 1, 2, 1)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func containsAnySub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// compile-time: NewPrometheusClient returns something satisfying the handler's
// PrometheusQueryClient interface.
var _ PrometheusQueryClient = (*prometheusClient)(nil)

// silence unused imports if the file is trimmed during refactor.
var _ = url.Values{}
