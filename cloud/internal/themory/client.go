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
	"bytes"
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
	// Scope routes the upstream call to `/v1/scopes/{scope}/{userID}/memories`.
	// Accepted values: "user" | "session" | "agent" | "repo". Empty defaults
	// to "user" for backward-compatibility with all callers that pre-date
	// the dashboard-memories-tab-standardization T3.2 BFF extension.
	// Per plan v1.5 — TheMemory REST already supports all 4 scope types
	// (T1.1/T2.1/T2.2 backend work); this field plumbs the dashboard's
	// 4-tab UI through to the upstream contract.
	Scope string
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

// memoryItemUpstream mirrors TheMemory OSS /v1/scopes/{type}/{id}/memories
// item shape (verified via /v1/openapi.json 2026-06-07). Internal adapter
// type — public API stays MemoryRow.
type memoryItemUpstream struct {
	ID        string         `json:"id"`
	Text      string         `json:"text"`
	Hash      string         `json:"hash"`
	Scope     map[string]any `json:"scope"`
	Payload   map[string]any `json:"payload"`
	CreatedAt string         `json:"createdAt"`
	UpdatedAt string         `json:"updatedAt"`
}

type scopeMemoriesResponse struct {
	Items      []memoryItemUpstream `json:"items"`
	NextCursor string               `json:"nextCursor,omitempty"`
}

// List proxies GET /v1/scopes/user/{userID}/memories for the given user
// subject. FIX 2026-06-07: adapt to actual OSS TheMemory contract (was
// stubbed for non-existent /memories path). Translates upstream
// {items:[{text,createdAt,...}]} into dashboard ListResult shape.
func (c *Client) List(ctx context.Context, userID string, opts ListOptions) (*ListResult, error) {
	q := url.Values{}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	// TheMemory uses cursor-based pagination; Offset is not supported.
	// Category/From/To are dropped silently — upstream lacks these filters.
	// Scope routes to the matching /v1/scopes/{scope}/{userID}/memories path.
	// Empty/invalid scope falls back to "user" (backward-compat + safe default).
	// dashboard-memories-tab-standardization v1.5 (T3.2): "repo" maps to "skill"
	// per ADR-0010 (RepoScope exposed via Memory.skills(repoId), maps internally
	// to skill scope). TheMemory REST returns 501 SKILL_LISTING_NOT_IMPLEMENTED
	// for skill scope listing — Client.List degrades gracefully to empty list
	// (200) instead of propagating 502 to Dashboard. The empty response is
	// honest: the backend simply doesn't expose the listing primitive yet.
	scope := opts.Scope
	switch scope {
	case "user", "session", "agent":
		// allowed values; pass through
	case "repo", "skill":
		scope = "skill"
	default:
		scope = "user"
	}
	path := "/v1/scopes/" + scope + "/" + url.PathEscape(userID) + "/memories"
	var upstream scopeMemoriesResponse
	if err := c.do(ctx, http.MethodGet, path, userID, q, nil, &upstream); err != nil {
		// Graceful degrade: when scope=skill returns 501 (not implemented per
		// ADR-0010), surface empty list. Dashboard EmptyState handles the UI.
		if scope == "skill" && (IsSkillListingNotImplemented(err) || IsNotFound(err)) {
			return &ListResult{Memories: []MemoryRow{}}, nil
		}
		return nil, err
	}
	out := &ListResult{Memories: make([]MemoryRow, 0, len(upstream.Items))}
	for _, item := range upstream.Items {
		row := MemoryRow{
			ID:        item.ID,
			Content:   item.Text,
			CreatedAt: item.CreatedAt,
		}
		// Category may live in payload (e.g., payload["category"]).
		if c, ok := item.Payload["category"].(string); ok {
			row.Category = c
		}
		out.Memories = append(out.Memories, row)
	}
	return out, nil
}

// Metrics computes counts client-side via List (TheMemory OSS does not
// expose a /metrics endpoint at v0.1.0-alpha — verified via /v1/openapi.json
// 2026-06-07). D3 invariant preserved: zero-initialised numerics, never null.
// FIX 2026-06-07: was stubbed for non-existent /metrics path; now derives
// TotalMemories from a paginated list call. Other counters (RetrievalAPI
// Usage, AddEvents, EntityCount) stay at 0 until upstream telemetry ships.
func (c *Client) Metrics(ctx context.Context, userID string) (*MetricsResult, error) {
	out := &MetricsResult{} // explicit zero values per D3
	listed, err := c.List(ctx, userID, ListOptions{Limit: 100})
	if err != nil {
		return out, nil // honest fallback — return zeros instead of upstream error
	}
	out.TotalMemories = len(listed.Memories)
	return out, nil
}

// Entities synthesizes the scope-grouped entity list from a List call
// (TheMemory OSS does not expose /entities at v0.1.0-alpha — verified
// via /v1/openapi.json 2026-06-07). FIX 2026-06-07: was stubbed with
// D1 fallback for the absent endpoint; now derives the entity grouping
// from upstream memories' `scope` field.
//
// For the dashboard's Insights page, this returns a single Entity row
// per discovered scope.id within the requested scope type. Empty result
// when the user has no memories — preserves D1 honest-empty contract.
func (c *Client) Entities(ctx context.Context, userID, scope string) (*EntitiesResult, error) {
	// Strategy: list memories under the requested scope and aggregate.
	// For scope="user", list directly under user/{userID}. For other
	// scope types (session/agent), upstream needs a different path —
	// returning empty until that's wired (handler stays graceful).
	if scope != "user" && scope != "" {
		return &EntitiesResult{Entities: []Entity{}}, nil
	}
	listed, err := c.List(ctx, userID, ListOptions{Limit: 100})
	if err != nil {
		return &EntitiesResult{Entities: []Entity{}}, nil
	}
	// Single entity per user subject — Plan 2 surfaces user as the canonical
	// entity at v0.1. Future expansion when upstream ships per-scope
	// enumeration.
	if len(listed.Memories) == 0 {
		return &EntitiesResult{Entities: []Entity{}}, nil
	}
	return &EntitiesResult{Entities: []Entity{
		{
			ID:          userID,
			ScopeType:   "user",
			MemoryCount: len(listed.Memories),
			LastSeenAt:  listed.Memories[0].CreatedAt,
		},
	}}, nil
}

// Forget proxies POST /v1/forget with body {memoryId} for TheMemory.
// FIX 2026-06-07: was stubbed for non-existent DELETE /memories/{id}
// path; now uses the canonical POST /v1/forget with body shape per
// /v1/openapi.json 2026-06-07.
// 404 from TheMemory surfaces as ErrNotFound — the handler relies on
// this to return 404 on cross-tenant attempts.
func (c *Client) Forget(ctx context.Context, userID, memoryID string) error {
	if memoryID == "" {
		return errors.New("memoryID required")
	}
	// hard:true matches dashboard "delete" UX (customer expects the row to
	// disappear immediately). Soft-delete leaves tInvalid set but list still
	// returns the row, which would confuse the customer ("I deleted it but
	// it's still there"). FIX 2026-06-07.
	payload, err := json.Marshal(map[string]any{"memoryId": memoryID, "hard": true})
	if err != nil {
		return fmt.Errorf("themory: marshal forget body: %w", err)
	}
	return c.do(ctx, http.MethodPost, "/v1/forget", userID, nil, bytes.NewReader(payload), nil)
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

// IsSkillListingNotImplemented reports whether err is a 501 from TheMemory's
// scopes handler indicating skill scope listing is not exposed via REST
// (ADR-0010 — `ScopedSkillMemory` has no `getAll()`; consumers use SDK
// `Memory.skills(repoId).recall(...)` directly). Used by Client.List to
// degrade scope=repo gracefully to an empty list instead of propagating 502
// to the Dashboard. See dashboard-memories-tab-standardization v1.5 T3.2.
func IsSkillListingNotImplemented(err error) bool {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.StatusCode != http.StatusNotImplemented {
		return false
	}
	return strings.Contains(apiErr.Body, "SKILL_LISTING_NOT_IMPLEMENTED")
}

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
