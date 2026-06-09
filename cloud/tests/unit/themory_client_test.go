// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Plan 1 of dashboard-memory-section-roadmap.md, T1.2.
// TheMemory HTTP client tests — load-bearing because the X-LoopZero-User-Id
// invariant (D1 of plan + CLAUDE.md mandate Invariant 3) lives here.
// If this client ever omits the header or sends a wrong subject value,
// tenant isolation collapses.

package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/loopzero/loopzero/cloud/internal/themory"
)

// stubMemoryServer captures inbound requests so tests can assert headers,
// path, and query params. Returns the configurable response body + status.
type stubMemoryServer struct {
	*httptest.Server
	lastRequest *http.Request
	lastBody    []byte
	respStatus  int
	respBody    string
}

func newStubMemoryServer(t *testing.T) *stubMemoryServer {
	t.Helper()
	s := &stubMemoryServer{respStatus: 200, respBody: `{}`}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.lastRequest = r.Clone(context.Background())
		w.WriteHeader(s.respStatus)
		_, _ = w.Write([]byte(s.respBody))
	}))
	t.Cleanup(s.Close)
	return s
}

// T1.2 invariant 1: every outbound request carries Authorization Bearer
// with the master TheMemory API key. The customer's loopzero_live_* key is
// NEVER sent to TheMemory — that's the whole point of the Go proxy.
func TestTheMemoryClient_SendsMasterBearerOnList(t *testing.T) {
	stub := newStubMemoryServer(t)
	stub.respBody = `{"memories": []}`

	client := themory.NewClient(stub.URL, "master-key-secret", nil)
	if _, err := client.List(context.Background(), "user-A", themory.ListOptions{Limit: 10}); err != nil {
		t.Fatalf("List: %v", err)
	}

	got := stub.lastRequest.Header.Get("Authorization")
	if got != "Bearer master-key-secret" {
		t.Errorf("Authorization header = %q, want %q", got, "Bearer master-key-secret")
	}
}

// T1.2 invariant 2 (CRITICAL — D1 + Invariant 3 of CLAUDE.md mandate):
// every outbound request carries X-LoopZero-User-Id derived from the
// authenticated session. Cross-tenant leakage cannot happen if this is
// always set + always derived from auth.GetUser(ctx).Subject.
func TestTheMemoryClient_InjectsXLoopZeroUserIDOnList(t *testing.T) {
	stub := newStubMemoryServer(t)
	stub.respBody = `{"memories": []}`

	client := themory.NewClient(stub.URL, "k", nil)
	if _, err := client.List(context.Background(), "user-alex-from-auth", themory.ListOptions{}); err != nil {
		t.Fatalf("List: %v", err)
	}

	got := stub.lastRequest.Header.Get("X-LoopZero-User-Id")
	if got != "user-alex-from-auth" {
		t.Errorf("X-LoopZero-User-Id header = %q, want %q (D1 isolation invariant)", got, "user-alex-from-auth")
	}
}

// T1.2 invariant 3 — same X-LoopZero-User-Id contract for Metrics + Forget.
func TestTheMemoryClient_InjectsXLoopZeroUserIDOnMetrics(t *testing.T) {
	stub := newStubMemoryServer(t)
	stub.respBody = `{"total_memories": 0}`

	client := themory.NewClient(stub.URL, "k", nil)
	if _, err := client.Metrics(context.Background(), "user-B"); err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	if got := stub.lastRequest.Header.Get("X-LoopZero-User-Id"); got != "user-B" {
		t.Errorf("Metrics X-LoopZero-User-Id = %q, want user-B", got)
	}
}

func TestTheMemoryClient_InjectsXLoopZeroUserIDOnForget(t *testing.T) {
	stub := newStubMemoryServer(t)
	stub.respStatus = 204

	client := themory.NewClient(stub.URL, "k", nil)
	if err := client.Forget(context.Background(), "user-C", "mem-1234"); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if got := stub.lastRequest.Header.Get("X-LoopZero-User-Id"); got != "user-C" {
		t.Errorf("Forget X-LoopZero-User-Id = %q, want user-C", got)
	}
}

// T1.2 invariant 4 — pagination params (limit + offset + cursor) propagate
// to the upstream request as query params. Without this, the dashboard's
// pagination is purely cosmetic.
func TestTheMemoryClient_PropagatesPaginationParams(t *testing.T) {
	stub := newStubMemoryServer(t)
	stub.respBody = `{"memories": []}`

	client := themory.NewClient(stub.URL, "k", nil)
	_, err := client.List(context.Background(), "u", themory.ListOptions{Limit: 25, Offset: 50, Category: "preferences"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	q := stub.lastRequest.URL.Query()
	if q.Get("limit") != "25" {
		t.Errorf("limit query = %q, want 25", q.Get("limit"))
	}
	if q.Get("offset") != "50" {
		t.Errorf("offset query = %q, want 50", q.Get("offset"))
	}
	if q.Get("category") != "preferences" {
		t.Errorf("category query = %q, want preferences", q.Get("category"))
	}
}

// dashboard-memories-tab-standardization T3.2 — List() with Scope option
// routes to /v1/scopes/{scope}/{userID}/memories (was hardcoded to "user").
// Closes Plan v1.5 BFF reality-check: 4-tab Memories dashboard requires
// BFF to pass through SESSION/AGENT/REPO scopes (TheMemory REST already
// supports all 4 at /v1/scopes/{type}/{id}/memories).
func TestTheMemoryClient_ListWithScopeRoutesToCorrectPath(t *testing.T) {
	cases := []struct {
		scope    string
		wantPath string
	}{
		{"", "/v1/scopes/user/u/memories"},        // empty defaults to user (backward-compat)
		{"user", "/v1/scopes/user/u/memories"},
		{"session", "/v1/scopes/session/u/memories"},
		{"agent", "/v1/scopes/agent/u/memories"},
		{"repo", "/v1/scopes/repo/u/memories"},
	}
	for _, tc := range cases {
		t.Run("scope="+tc.scope, func(t *testing.T) {
			stub := newStubMemoryServer(t)
			stub.respBody = `{"items": []}`
			client := themory.NewClient(stub.URL, "k", nil)
			if _, err := client.List(context.Background(), "u", themory.ListOptions{Scope: tc.scope}); err != nil {
				t.Fatalf("List: %v", err)
			}
			if got := stub.lastRequest.URL.Path; got != tc.wantPath {
				t.Errorf("scope=%q routed to %q, want %q", tc.scope, got, tc.wantPath)
			}
		})
	}
}

// dashboard-memories-tab-standardization T4.1 — BFF pass-through smoke.
// Asserts the BFF List() surfaces the EXACT createdAt string returned by
// upstream TheMemory without overwriting, reformatting, OR re-stamping.
// This is the load-bearing contract that closes Sub-goal 6 of the plan —
// if BFF time-stamps client-side, the Dashboard sees lies even though the
// backend (T2.1+T2.2) projects real DB timestamps.
func TestTheMemoryClient_ListPassesThroughUpstreamCreatedAt(t *testing.T) {
	cases := []struct {
		name        string
		scope       string
		upstreamISO string
	}{
		{"user_scope_recent", "user", "2026-06-09T15:00:00.000Z"},
		{"user_scope_historical", "user", "2024-01-15T10:30:00.000Z"},
		{"session_scope_recent", "session", "2026-06-09T16:00:00.000Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := newStubMemoryServer(t)
			// Upstream returns one row with the controlled createdAt — the
			// BFF MUST surface this string verbatim through to the client.
			stub.respBody = `{"items": [{
				"id": "mem-abc123",
				"text": "test fact",
				"hash": "h1",
				"scope": {"user_id": "alice"},
				"payload": {},
				"createdAt": "` + tc.upstreamISO + `",
				"updatedAt": "` + tc.upstreamISO + `"
			}]}`

			client := themory.NewClient(stub.URL, "k", nil)
			result, err := client.List(context.Background(), "alice", themory.ListOptions{Scope: tc.scope})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(result.Memories) != 1 {
				t.Fatalf("expected 1 row, got %d", len(result.Memories))
			}
			if got := result.Memories[0].CreatedAt; got != tc.upstreamISO {
				t.Errorf("CreatedAt pass-through broken: got %q, want %q (BFF must NOT rewrite upstream timestamps)", got, tc.upstreamISO)
			}
		})
	}
}

// T1.2 invariant 5 (D4): traceparent W3C header propagates from request
// context into the outbound HTTP call so a single trace ID traverses
// browser → Go → TheMemory. The otelhttp.NewTransport wrapping handles
// this automatically — we assert that the client honors the default
// http.Client when one is provided, so production wiring with otelhttp
// works without surgery here.
func TestTheMemoryClient_UsesProvidedHTTPClient(t *testing.T) {
	stub := newStubMemoryServer(t)
	stub.respBody = `{"memories": []}`

	// A custom Transport observes that the round-trip happened — proof the
	// client did not bypass the provided *http.Client for some default one.
	called := false
	custom := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			called = true
			return http.DefaultTransport.RoundTrip(r)
		}),
	}
	client := themory.NewClient(stub.URL, "k", custom)
	if _, err := client.List(context.Background(), "u", themory.ListOptions{}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if !called {
		t.Error("client did not use the provided *http.Client — otelhttp.NewTransport would be bypassed in prod (D4 violation)")
	}
}

// T1.2 invariant 6: TheMemory returns 4xx → Go client surfaces a typed
// error consumers can branch on. ErrNotFound for 404 specifically (Forget
// handler relies on this to return 404 on cross-tenant attempts).
func TestTheMemoryClient_404OnForgetReturnsErrNotFound(t *testing.T) {
	stub := newStubMemoryServer(t)
	stub.respStatus = 404
	stub.respBody = `{"error": "not found"}`

	client := themory.NewClient(stub.URL, "k", nil)
	err := client.Forget(context.Background(), "u", "mem-doesnt-exist")
	if err == nil {
		t.Fatal("expected error on 404, got nil")
	}
	if !themory.IsNotFound(err) {
		t.Errorf("expected themory.IsNotFound to match, got %v", err)
	}
}

// roundTripperFunc adapts a function to the http.RoundTripper interface
// so tests can introspect the round-trip without writing a struct.
type roundTripperFunc func(r *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// =============================================================================
// Plan 2 — Entities() method extension (T2.2)
// =============================================================================

// T2.2 invariant 1: Entities call carries the same Bearer + X-LoopZero-User-Id
// invariants as List/Metrics/Forget (Plan 1 D1 propagated).
func TestTheMemoryClient_EntitiesSendsBearerAndUserID(t *testing.T) {
	stub := newStubMemoryServer(t)
	stub.respBody = `{"entities": []}`

	client := themory.NewClient(stub.URL, "master-key", nil)
	if _, err := client.Entities(context.Background(), "user-Z", "user"); err != nil {
		t.Fatalf("Entities: %v", err)
	}
	if got := stub.lastRequest.Header.Get("Authorization"); got != "Bearer master-key" {
		t.Errorf("Authorization = %q, want Bearer master-key", got)
	}
	if got := stub.lastRequest.Header.Get("X-LoopZero-User-Id"); got != "user-Z" {
		t.Errorf("X-LoopZero-User-Id = %q, want user-Z (D1 isolation violation if drift)", got)
	}
}

// T2.2 invariant 2: scope param propagates to TheMemory as ?scope=<type>.
func TestTheMemoryClient_EntitiesPropagatesScopeQuery(t *testing.T) {
	stub := newStubMemoryServer(t)
	stub.respBody = `{"entities": []}`

	client := themory.NewClient(stub.URL, "k", nil)
	if _, err := client.Entities(context.Background(), "u", "session"); err != nil {
		t.Fatalf("Entities: %v", err)
	}
	if got := stub.lastRequest.URL.Query().Get("scope"); got != "session" {
		t.Errorf("scope query = %q, want session", got)
	}
}

// T2.2 invariant 3: TheMemory returns 404 (endpoint not yet shipped at OSS
// sibling) → client surfaces empty result, NOT error. Per D1 fallback: the
// caller (handler) can decide to compute aggregation client-side from
// /memories. Surfacing as a typed empty avoids the handler having to
// distinguish "true zero" from "endpoint missing".
func TestTheMemoryClient_Entities404ReturnsEmptyNotError(t *testing.T) {
	stub := newStubMemoryServer(t)
	stub.respStatus = 404

	client := themory.NewClient(stub.URL, "k", nil)
	result, err := client.Entities(context.Background(), "u", "user")
	if err != nil {
		t.Fatalf("expected nil error on 404 (D1 fallback path), got %v", err)
	}
	if result == nil || len(result.Entities) != 0 {
		t.Errorf("expected empty Entities slice, got %+v", result)
	}
}
