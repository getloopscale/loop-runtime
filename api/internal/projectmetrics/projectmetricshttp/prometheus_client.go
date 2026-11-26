package projectmetricshttp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// prometheusClient is the concrete PrometheusQueryClient (J-18 T1.2). It executes
// Prometheus `query_range` over HTTP, structurally mirroring the api/internal/logs/loki
// client. Errors are SCRUBBED — the PromQL (which carries tenant_id + project
// labels) is never echoed into an error or log.
type prometheusClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewPrometheusClient builds a client against a Prometheus HTTP API root, e.g.
// "http://prometheus.monitoring:9090". A nil httpClient gets a default with a
// bounded 10s timeout (the handler additionally applies a per-request 5s ctx
// timeout — the tighter of the two wins).
func NewPrometheusClient(baseURL string, httpClient *http.Client) PrometheusQueryClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &prometheusClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *prometheusClient) QueryRange(ctx context.Context, promQL string, startUnix, endUnix int64, stepSeconds int) ([]PrometheusDataPoint, error) {
	params := url.Values{}
	params.Set("query", promQL)
	params.Set("start", strconv.FormatInt(startUnix, 10))
	params.Set("end", strconv.FormatInt(endUnix, 10))
	params.Set("step", strconv.Itoa(stepSeconds))
	reqURL := c.baseURL + "/api/v1/query_range?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("prometheus: request build failed")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Context cancellation/deadline surfaces here — return it verbatim so
		// the handler can map it to 504. Otherwise a scrubbed generic error.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("prometheus: request failed")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus: unexpected status %d", resp.StatusCode)
	}

	var pr struct {
		Data struct {
			Result []struct {
				Values [][2]json.RawMessage `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, fmt.Errorf("prometheus: decode failed")
	}
	// Empty result (project < 24h old, no series) is NOT an error — the handler
	// maps a nil/empty slice to `{ status: "collecting" }`.
	if len(pr.Data.Result) == 0 {
		return nil, nil
	}

	// A per-project/tenant query yields a single series; take the first.
	raw := pr.Data.Result[0].Values
	out := make([]PrometheusDataPoint, 0, len(raw))
	for _, v := range raw {
		// Prometheus value pair: v[0] = timestamp (number, unix seconds),
		// v[1] = value (string, e.g. "0.99").
		var ts float64
		if err := json.Unmarshal(v[0], &ts); err != nil {
			continue
		}
		var valStr string
		if err := json.Unmarshal(v[1], &valStr); err != nil {
			continue
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		// loopruntime#180: Prometheus serializes NaN/Inf as "NaN"/"+Inf"/"-Inf", which
		// ParseFloat accepts WITHOUT error. A non-finite sample (e.g. error_rate
		// 0/0 for an idle-but-has-history tenant) is never a displayable metric
		// value — drop it so an all-non-finite series collapses to empty and the
		// handler reports "collecting" instead of surfacing "NaN%".
		if math.IsNaN(val) || math.IsInf(val, 0) {
			continue
		}
		out = append(out, PrometheusDataPoint{Timestamp: int64(ts), Value: val})
	}
	return out, nil
}
