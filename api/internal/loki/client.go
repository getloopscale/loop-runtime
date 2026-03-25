// Package loki provides an HTTP client for querying Grafana Loki.
// Used by the logs handler to retrieve build output from Argo Workflows pods.
package loki

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/loopzero/loopzero/api/internal/routes"
)

// Client queries Grafana Loki for build logs via the HTTP API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewClient creates a Loki client.
// baseURL is the Loki HTTP API root, e.g., "http://loki.monitoring:3100".
func NewClient(baseURL string, httpClient *http.Client, logger *slog.Logger) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
		logger:     logger,
	}
}

// QueryBuildLogs queries Loki for logs of a specific build workflow.
// Implements routes.LogQuerier.
func (c *Client) QueryBuildLogs(ctx context.Context, buildID string, opts routes.LogQueryOpts) ([]routes.LogEntry, error) {
	// Build LogQL query
	// Argo Workflow names contain the build ID plus a random suffix:
	// "loopzero-build-{buildID}-{suffix}". Use regex match to find by build ID.
	query := fmt.Sprintf(`{workflow=~".*%s.*"}`, buildID)
	if opts.Step != "" {
		query = fmt.Sprintf(`{workflow=~".*%s.*", step="%s"}`, buildID, opts.Step)
	}

	limit := 1000
	if opts.Limit > 0 {
		limit = opts.Limit
	}

	// Query Loki API: GET /loki/api/v1/query_range
	params := url.Values{
		"query":     {query},
		"limit":     {strconv.Itoa(limit)},
		"direction": {"forward"},
		"start":     {strconv.FormatInt(time.Now().Add(-7*24*time.Hour).UnixNano(), 10)},
		"end":       {strconv.FormatInt(time.Now().UnixNano(), 10)},
	}

	reqURL := fmt.Sprintf("%s/loki/api/v1/query_range?%s", c.baseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("loki: create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Warn("loki query failed", "error", err, "build_id", buildID)
		return nil, fmt.Errorf("loki: query: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		c.logger.Warn("loki non-200 response", "status", resp.StatusCode, "body", string(body), "build_id", buildID)
		return nil, fmt.Errorf("loki: unexpected status %d", resp.StatusCode)
	}

	var lokiResp lokiQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&lokiResp); err != nil {
		return nil, fmt.Errorf("loki: decode response: %w", err)
	}

	return parseLokiStreams(lokiResp), nil
}

// lokiQueryResponse is the Loki API response structure.
type lokiQueryResponse struct {
	Status string         `json:"status"`
	Data   lokiResultData `json:"data"`
}

type lokiResultData struct {
	ResultType string       `json:"resultType"`
	Result     []lokiStream `json:"result"`
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"` // [[nanosecond_timestamp, log_line], ...]
}

// parseLokiStreams converts Loki stream results into LogEntry slices.
func parseLokiStreams(resp lokiQueryResponse) []routes.LogEntry {
	var entries []routes.LogEntry

	for _, stream := range resp.Data.Result {
		step := stream.Stream["step"]
		if step == "" {
			step = stream.Stream["container"]
		}
		streamType := "stdout"

		for _, value := range stream.Values {
			if len(value) < 2 {
				continue
			}
			nsTimestamp := value[0]
			content := value[1]

			ts := parseNanosecondTimestamp(nsTimestamp)

			entries = append(entries, routes.LogEntry{
				Step:      step,
				Stream:    streamType,
				Content:   content,
				Timestamp: ts,
			})
		}
	}

	// Sort by timestamp ascending
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp < entries[j].Timestamp
	})

	return entries
}

// parseNanosecondTimestamp converts Loki's nanosecond string timestamp to RFC3339.
func parseNanosecondTimestamp(ns string) string {
	nsInt, err := strconv.ParseInt(ns, 10, 64)
	if err != nil {
		return ns
	}
	t := time.Unix(0, nsInt)
	return t.UTC().Format(time.RFC3339)
}
