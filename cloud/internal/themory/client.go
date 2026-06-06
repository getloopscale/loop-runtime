// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package themory is the thin HTTP client LoopZeroCloud uses to reach OSS
// TheMemory. It is deliberately small — the contract between LoopZeroCloud
// and TheMemory is HTTP + headers, NOT a fat SDK with object mappers
// (D1 of Plan 1: 2026-06-06-memory-shell-plan.md).
//
// The load-bearing invariants:
//
//   1. Every outbound request carries `Authorization: Bearer <master-key>`.
//      The customer's loopzero_live_* key NEVER leaves the Go process
//      (CLAUDE.md "GO-TO CTO mandate" Invariant 3 + Plan 1 ADR D1).
//
//   2. Every outbound request carries `X-LoopZero-User-Id: <subject>` where
//      subject is auth.GetUser(ctx).Subject from the inbound JWT/scope
//      check. This is how tenant isolation is enforced — TheMemory
//      itself is tenant-unaware at v0.1 (per CLAUDE.md mandate), so the
//      Go proxy is the only thing keeping tenant A's calls from seeing
//      tenant B's data.
//
//   3. The provided *http.Client passes through verbatim. Production
//      wires this with otelhttp.NewTransport so W3C traceparent headers
//      propagate browser → Go → TheMemory (D4 of Plan 1).
package themory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Client talks to TheMemory's REST API. Construct via NewClient.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient constructs a TheMemory client. baseURL is the root of the
// TheMemory service (e.g., http://themory:3000). apiKey is the master
// THEMORY_API_KEY — held by the Go service, NEVER returned to the
// customer. httpClient may be nil — when nil, http.DefaultClient is used
// (suitable for tests; production should pass an otelhttp-wrapped client
// for D4 trace propagation).
func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: httpClient,
	}
}

// ListOptions are the query params accepted by GET /memories.
type ListOptions struct {
	Limit    int
	Offset   int
	Category string // optional filter; empty means no filter
	From     string // RFC3339; empty means no lower bound
	To       string // RFC3339; empty means no upper bound
}

// MemoryRow is one entry in the List response. Field shape is a subset
// of what TheMemory may return — Go intentionally accepts more than it
// uses (Postel's law) but the dashboard renders only the documented set.
type MemoryRow struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Category  string `json:"category,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// ListResult is the GET /memories response shape.
type ListResult struct {
	Memories   []MemoryRow `json:"memories"`
	NextOffset *int        `json:"next_offset,omitempty"`
	Total      *int        `json:"total,omitempty"`
}

// MetricsResult is the GET /metrics response shape. Per D3 of Plan 1,
// fields are always numeric — empty pgvector returns 0, NEVER null.
type MetricsResult struct {
	TotalMemories     int `json:"total_memories"`
	RetrievalAPIUsage int `json:"retrieval_api_usage"`
	AddEvents         int `json:"add_events"`
	EntityCount       int `json:"entity_count"`
}

// Entity is one row in the Entities response — an aggregation over the
// customer's memories by (scope_type, scope_id). Plan 2 D1: derived state,
// NOT a backing table.
type Entity struct {
	ID          string `json:"id"`           // scope-typed id (e.g. "alex" under scope=user)
	ScopeType   string `json:"scope_type"`   // user | session | agent | repo
	MemoryCount int    `json:"memory_count"` // 0 for empty entity (D3 inheritance)
	LastSeenAt  string `json:"last_seen_at,omitempty"`
}

// EntitiesResult is the GET /entities response shape.
type EntitiesResult struct {
	Entities []Entity `json:"entities"`
}

// List proxies GET /memories for the given user subject.
func (c *Client) List(ctx context.Context, userID string, opts ListOptions) (*ListResult, error) {
	q := url.Values{}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		q.Set("offset", strconv.Itoa(opts.Offset))
	}
	if opts.Category != "" {
		q.Set("category", opts.Category)
	}
	if opts.From != "" {
		q.Set("from", opts.From)
	}
	if opts.To != "" {
		q.Set("to", opts.To)
	}
	var out ListResult
	if err := c.do(ctx, http.MethodGet, "/memories", userID, q, nil, &out); err != nil {
		return nil, err
	}
	// D3 invariant: never return nil slice for empty result — pass-through
	// from TheMemory would be `null` in some implementations.
	if out.Memories == nil {
		out.Memories = []MemoryRow{}
	}
	return &out, nil
}

// Metrics proxies GET /metrics for the given user subject. Empty data
// returns zero-initialised values per D3 (NOT NaN, NOT null).
func (c *Client) Metrics(ctx context.Context, userID string) (*MetricsResult, error) {
	out := &MetricsResult{} // explicit zero values per D3
	if err := c.do(ctx, http.MethodGet, "/metrics", userID, nil, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Entities proxies GET /entities?scope=<type> for the given user subject.
// Per Plan 2 D1, when TheMemory's /entities endpoint is absent (404), the
// client surfaces an empty result so the handler / dashboard can either
// fall back to client-side aggregation or render an empty state cleanly —
// 404 is NOT propagated as an error from this method.
func (c *Client) Entities(ctx context.Context, userID, scope string) (*EntitiesResult, error) {
	q := url.Values{}
	q.Set("scope", scope)
	var out EntitiesResult
	err := c.do(ctx, http.MethodGet, "/entities", userID, q, nil, &out)
	if err != nil && errors.Is(err, ErrNotFound) {
		// D1 fallback: TheMemory's /entities endpoint may not exist yet
		// at the upstream OSS sibling's frozen P0. Empty result is the
		// honest signal — handler can decide what to do next.
		return &EntitiesResult{Entities: []Entity{}}, nil
	}
	if err != nil {
		return nil, err
	}
	if out.Entities == nil {
		out.Entities = []Entity{}
	}
	return &out, nil
}

// Forget proxies DELETE /memories/{id} for the given user subject.
// 404 from TheMemory surfaces as ErrNotFound — the handler relies on
// this to return 404 on cross-tenant attempts (since TheMemory will
// not find the memory under the wrong user-id).
func (c *Client) Forget(ctx context.Context, userID, memoryID string) error {
	if memoryID == "" {
		return errors.New("memoryID required")
	}
	path := "/memories/" + url.PathEscape(memoryID)
	return c.do(ctx, http.MethodDelete, path, userID, nil, nil, nil)
}

// do is the shared request lifecycle. Sets the canonical headers
// (Authorization, X-LoopZero-User-Id, Accept), executes via the configured
// http.Client (which prod wraps with otelhttp for D4 propagation),
// inspects the response for HTTP status, and decodes JSON into out
// when out is non-nil.
func (c *Client) do(ctx context.Context, method, path, userID string, query url.Values, body io.Reader, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return fmt.Errorf("themory: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-LoopZero-User-Id", userID)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("themory: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &Error{StatusCode: resp.StatusCode, Body: string(raw)}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("themory: decode response: %w", err)
	}
	return nil
}

// ErrNotFound is returned for 404 responses. Forget handler maps this
// directly to HTTP 404 for the customer (consistent with the Plan 0 D5
// existence-leak rule — same response shape for "doesn't exist anywhere"
// and "exists under a different user-id").
var ErrNotFound = errors.New("themory: not found")

// IsNotFound reports whether err is or wraps ErrNotFound.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// Error is a typed wrapper for non-2xx responses other than 404.
// Exposes the upstream status + body for slog without leaking secrets
// (the body never contains the master Bearer — that's in headers, which
// are not echoed back by TheMemory).
type Error struct {
	StatusCode int
	Body       string
}

func (e *Error) Error() string {
	return fmt.Sprintf("themory: upstream status %d: %s", e.StatusCode, e.Body)
}
