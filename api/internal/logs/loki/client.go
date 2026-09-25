// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/api/internal/logs"
	"github.com/getloopscale/loopzero/pkg/naming"
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
// Implements ports.LogQuerier.
func (c *Client) QueryBuildLogs(ctx context.Context, buildID string, opts logs.LogQueryOpts) ([]logs.LogEntry, error) {
	// SEC-005 fix: Validate buildID and step to prevent LogQL injection.
	// Only allow alphanumeric, hyphens, and underscores.
	if !isValidLogParam(buildID) {
		return nil, fmt.Errorf("loki: invalid build ID: %q", buildID)
	}
	if opts.Step != "" && !isValidLogParam(opts.Step) {
		return nil, fmt.Errorf("loki: invalid step parameter: %q", opts.Step)
	}

	// Build LogQL query
	// Argo Workflow names contain the build ID plus a random suffix:
	// "loopzero-build-{buildID}-{suffix}". Use regex match to find by build ID.
	//
	// Plan J-17 Layer 3 (2026-06-06): default (Debug=false) restricts
	// the stream selector to lines that carry the `loopzero_phase` label —
	// i.e. only lines emitted by the Argo pipeline's ::loopzero:: marker
	// wrappers. This excludes container=wait/init (already dropped by
	// Promtail) AND raw main-container stdout from the user view,
	// surfacing only curated user-facing milestones. Debug=true
	// bypasses the filter and returns everything Loki retained
	// (operator-only — handler enforces auth gate).
	selector := fmt.Sprintf(`workflow=~".*%s.*"`, buildID)
	if !opts.Debug {
		selector = selector + `, loopzero_phase=~".+"`
	}
	query := fmt.Sprintf(`{%s}`, selector)
	if opts.Step != "" {
		query = fmt.Sprintf(`{%s, step="%s"}`, selector, opts.Step)
	}

	limit := 1000
	if opts.Limit > 0 {
		limit = opts.Limit
	}

	// Time window for the Loki query_range call.
	//   - Default: last 7 days (matches the chart's default retention,
	//     and the operator's mental model "show me recent build logs").
	//   - Beta Logs Gate Criterion 7: opts.SinceSeconds > 0 overrides
	//     so `loopzero logs --since 30d` works for older builds (subject
	//     to the cluster's actual Loki retention).
	var startNano int64
	if opts.SinceSeconds > 0 {
		startNano = time.Now().Add(-time.Duration(opts.SinceSeconds) * time.Second).UnixNano()
	} else {
		startNano = time.Now().Add(-7 * 24 * time.Hour).UnixNano()
	}

	return c.queryRange(ctx, query, limit, startNano, "build_id", buildID)
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

// parseLokiStreams converts Loki stream results into LogEntry slices ordered
// by the nanosecond timestamp Loki sent. The displayed Timestamp has second
// precision, so ordering on it would scramble lines within one second; a
// stable sort keeps a stream's own order for lines with the same nanosecond.
func parseLokiStreams(resp lokiQueryResponse) ([]logs.LogEntry, error) {
	type timedEntry struct {
		ns    int64
		entry logs.LogEntry
	}
	var timed []timedEntry
	for _, stream := range resp.Data.Result {
		step := stream.Stream["step"]
		if step == "" {
			step = stream.Stream["container"]
		}
		for _, value := range stream.Values {
			if len(value) < 2 {
				continue
			}
			ns, err := strconv.ParseInt(value[0], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("loki: line timestamp %q is not nanoseconds: %w", value[0], err)
			}
			timed = append(timed, timedEntry{ns: ns, entry: logs.LogEntry{
				Step:      step,
				Stream:    "stdout",
				Content:   value[1],
				Timestamp: time.Unix(0, ns).UTC().Format(time.RFC3339),
			}})
		}
	}
	sort.SliceStable(timed, func(i, j int) bool { return timed[i].ns < timed[j].ns })

	entries := make([]logs.LogEntry, len(timed))
	for i, t := range timed {
		entries[i] = t.entry
	}
	return entries, nil
}

// logParamRegex allows only safe characters in LogQL query parameters.
// SEC-005: prevents LogQL injection via buildID or step parameters.
var logParamRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// isValidLogParam checks that a parameter is safe for interpolation into LogQL queries.
func isValidLogParam(s string) bool {
	return len(s) <= 128 && logParamRegex.MatchString(s)
}

// buildRuntimeLogQL parses a runtime queryKey and constructs the LogQL
// selector. Extracted from QueryBuildLogsSince so the LogQL shape is
// testable without an HTTP round-trip to Loki.
//
// Accepts BOTH the legacy 4-segment form (runtime:tenant:env:project)
// and the 5-segment form (runtime:tenant:env:project:app) added 2026-
// 05-13 for Beta Logs Gate per-app filtering.
//
// Empty {app} segment in the 5-segment form is treated as "no filter"
// (functionally identical to the 4-segment form). This keeps the
// HTTP handler from having to branch on empty values.
func buildRuntimeLogQL(queryKey string) (string, error) {
	parts := strings.SplitN(queryKey, ":", 5)
	if len(parts) < 4 {
		return "", fmt.Errorf("loki: invalid runtime query key: %q", queryKey)
	}
	tenant, env, project := parts[1], parts[2], parts[3]
	var app string
	if len(parts) == 5 {
		app = parts[4]
	}
	if !isValidLogParam(tenant) || !isValidLogParam(env) || !isValidLogParam(project) {
		return "", fmt.Errorf("loki: invalid runtime query parameters")
	}
	// T2.2 of plan 2026-06-11-code-review-remediation: post-M2 canonical
	// namespace formula is `loopzero-{tenant}-{project}-{env}` per
	// pkg/naming.Namespace (sponsor verbatim 2026-06-07, meaning "EVERYTHING
	// SHOULD be separated by project": "TUDO DEVERIA esta separado por projeto"). // english-only: verbatim sponsor quote, kept as evidence
	// Pre-T2.2 this client emitted only the
	// legacy `loopzero-{tenant}-{env}` form, so logs for any M2-shaped
	// namespace silently disappeared from /logs queries.
	//
	// The two-segment legacy alternative was deleted. It existed so pre-M2 logs stayed
	// visible, but tenant namespaces fall under Loki's default 168h retention (only
	// secrets-system carries the 90-day stream), and the M2 rename landed 2026-06-12 —
	// those logs expired weeks ago. An exact match also removes the prefix-collision
	// risk the anchored regex was there to contain.
	canonical, err := naming.Namespace(tenant, project, env)
	if err != nil {
		return "", fmt.Errorf("loki: namespace for %s/%s/%s: %w", tenant, project, env, err)
	}
	namespaceSelector := fmt.Sprintf(`namespace=%q`, canonical)
	if app != "" {
		if !isValidLogParam(app) {
			return "", fmt.Errorf("loki: invalid app filter parameter")
		}
		return fmt.Sprintf(`{%s, app="%s", container=~".+"}`, namespaceSelector, app), nil
	}
	return fmt.Sprintf(`{%s, container=~".+"}`, namespaceSelector), nil
}

// QueryBuildLogsSince queries Loki for logs newer than sinceNano (nanosecond timestamp).
// Used by the log streaming handler for incremental polling.
// Implements ports.StreamLogQuerier.
//
// The queryKey can be:
//   - A build ID (e.g., "bld-abc123") for build logs
//   - A runtime key "runtime:{tenant}:{env}:{project}" for runtime logs
//     (legacy 4-segment form — kept for backward compatibility)
//   - A runtime key "runtime:{tenant}:{env}:{project}:{app}" with an
//     optional 5th segment (Beta Logs Gate Criterion 3) — when {app}
//     is non-empty the LogQL selector includes `app="<value>"`.
func (c *Client) QueryBuildLogsSince(ctx context.Context, queryKey string, sinceNano int64, limit int) ([]logs.LogEntry, error) {
	var query string

	// Parse runtime key format: "runtime:{tenant}:{env}:{project}" or
	// "runtime:{tenant}:{env}:{project}:{app}" (5-segment form added
	// 2026-05-13 for per-app filtering).
	if strings.HasPrefix(queryKey, "runtime:") {
		var qerr error
		query, qerr = buildRuntimeLogQL(queryKey)
		if qerr != nil {
			return nil, qerr
		}
	} else {
		// Build logs: query by workflow name containing buildID
		if !isValidLogParam(queryKey) {
			return nil, fmt.Errorf("loki: invalid build ID: %q", queryKey)
		}
		query = fmt.Sprintf(`{workflow=~".*%s.*"}`, queryKey)
	}

	if limit <= 0 {
		limit = 500
	}

	// Start time: sinceNano + 1 (to avoid re-fetching the same line)
	startNano := sinceNano
	if startNano > 0 {
		startNano++
	} else {
		// Default: last 1 hour for streaming
		startNano = time.Now().Add(-1 * time.Hour).UnixNano()
	}

	return c.queryRange(ctx, query, limit, startNano, "key", queryKey)
}

// queryRange runs one forward query_range call from startNano to now and
// returns the lines in time order. logKey/logValue name the caller's subject
// in the warning logged on failure.
func (c *Client) queryRange(ctx context.Context, query string, limit int, startNano int64, logKey, logValue string) ([]logs.LogEntry, error) {
	params := url.Values{
		"query":     {query},
		"limit":     {strconv.Itoa(limit)},
		"direction": {"forward"},
		"start":     {strconv.FormatInt(startNano, 10)},
		"end":       {strconv.FormatInt(time.Now().UnixNano(), 10)},
	}
	reqURL := fmt.Sprintf("%s/loki/api/v1/query_range?%s", c.baseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("loki: create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Warn("loki query failed", "error", err, logKey, logValue)
		return nil, fmt.Errorf("loki: query: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		c.logger.Warn("loki non-200 response", "status", resp.StatusCode, "body", string(body), logKey, logValue)
		return nil, fmt.Errorf("loki: unexpected status %d", resp.StatusCode)
	}

	var lokiResp lokiQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&lokiResp); err != nil {
		return nil, fmt.Errorf("loki: decode response: %w", err)
	}
	return parseLokiStreams(lokiResp)
}
