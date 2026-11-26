// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Tempo OTel HTTP client implementing the AssertSpanCoverage helper
// promised by T3.3 of plan canonical-tests-rewrite-v2 v1.0 per ADR D7.
//
// Per blueprint P3: chaos test assertions check that traces show baseline
// + during-chaos + post-chaos span patterns. This client queries Tempo's
// /api/search endpoint using its native JSON-tag filter.

package chaos

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultTempoURL is the canonical in-cluster Tempo endpoint.
const DefaultTempoURL = "http://tempo.monitoring.svc.cluster.local:3200"

// tempoSearchResp models the relevant subset of Tempo's /api/search response.
type tempoSearchResp struct {
	Traces []struct {
		TraceID  string `json:"traceID"`
		RootSpan string `json:"rootSpan"`
		Spans    []struct {
			Name          string `json:"name"`
			DurationNanos int64  `json:"durationNanos"`
		} `json:"spans"`
	} `json:"traces"`
}

// FetchSpans queries Tempo for traces matching the service/operation filters
// over the time window. Returns parsed traces or error.
//
// If TempoTraceQuery.TempoURL is empty, falls back to DefaultTempoURL.
func FetchSpans(ctx context.Context, q TempoTraceQuery) (int, error) {
	tempoURL := q.TempoURL
	if tempoURL == "" {
		tempoURL = DefaultTempoURL
	}

	// Tempo /api/search takes ONE `tags` param in logfmt (space-separated
	// key="value" pairs). Repeating `tags=` (Set then Add) makes the server
	// read only the first, silently dropping the operation filter — so build
	// a single combined value instead.
	params := url.Values{}
	var filters []string
	if q.Service != "" {
		filters = append(filters, fmt.Sprintf(`service.name="%s"`, q.Service))
	}
	if q.Operation != "" {
		filters = append(filters, fmt.Sprintf(`name="%s"`, q.Operation))
	}
	if len(filters) > 0 {
		params.Set("tags", strings.Join(filters, " "))
	}
	if q.Window > 0 {
		params.Set("start", fmt.Sprintf("%d", time.Now().Add(-q.Window).Unix()))
		params.Set("end", fmt.Sprintf("%d", time.Now().Unix()))
	}

	req, err := http.NewRequestWithContext(ctx, "GET", tempoURL+"/api/search?"+params.Encode(), nil)
	if err != nil {
		return 0, fmt.Errorf("build tempo search req: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("tempo search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("tempo search status %d: %s", resp.StatusCode, string(body))
	}

	var parsed tempoSearchResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0, fmt.Errorf("decode tempo response: %w", err)
	}

	return len(parsed.Traces), nil
}

// AssertSpanCoverageReal wraps FetchSpans and asserts trace count >= minCount.
// Real implementation replacing the stub in helpers.go.
func AssertSpanCoverageReal(ctx context.Context, q TempoTraceQuery, minCount int) (int, error) {
	count, err := FetchSpans(ctx, q)
	if err != nil {
		// Network errors return -1 to keep distinct from "0 traces found".
		return -1, err
	}
	if count < minCount {
		return count, fmt.Errorf("span coverage below threshold: got %d traces matching service=%q op=%q, want >= %d",
			count, q.Service, q.Operation, minCount)
	}
	return count, nil
}
