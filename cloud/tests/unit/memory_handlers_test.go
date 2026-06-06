// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Plan 1 of dashboard-memory-section-roadmap.md, T1.3-T1.5.
// Tests for the 3 memory handlers (List / Metrics / Forget) — covering
// the auth boundary (RequireScope is plugged in main.go; here we test
// the handlers themselves get the user from ctx + delegate to the client),
// the D3 empty-state semantics, the D5-inherited cross-tenant 404, and
// the EC-2-style malformed-id-rejected-before-client invariant.

package unit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	"github.com/loopzero/loopzero/cloud/internal/routes"
	"github.com/loopzero/loopzero/cloud/internal/themory"
)

// fakeTheMemory captures inbound arguments + lets each test program a
// canned response. Implements the routes.TheMemoryClient interface so
// the handler can be wired without a real HTTP server.
type fakeTheMemory struct {
	lastUserID         string
	lastOpts           themory.ListOptions
	lastForget         string
	lastEntitiesScope  string

	listResp     *themory.ListResult
	listErr      error
	metricsResp  *themory.MetricsResult
	metricsErr   error
	forgetErr    error
	entitiesResp *themory.EntitiesResult
	entitiesErr  error
}

func (f *fakeTheMemory) List(_ context.Context, userID string, opts themory.ListOptions) (*themory.ListResult, error) {
	f.lastUserID = userID
	f.lastOpts = opts
	return f.listResp, f.listErr
}

func (f *fakeTheMemory) Metrics(_ context.Context, userID string) (*themory.MetricsResult, error) {
	f.lastUserID = userID
	return f.metricsResp, f.metricsErr
}

func (f *fakeTheMemory) Forget(_ context.Context, userID, memoryID string) error {
	f.lastUserID = userID
	f.lastForget = memoryID
	return f.forgetErr
}

func newMemoryHandler(t *testing.T) (*routes.MemoryHandler, *fakeTheMemory) {
	t.Helper()
	fake := &fakeTheMemory{}
	return routes.NewMemoryHandler(fake, nil), fake
}

// =============================================================================
// GET /v1/memory/list — T1.3
// =============================================================================

func TestMemoryList_RejectsUnauthenticated(t *testing.T) {
	h, _ := newMemoryHandler(t)
	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest("GET", "/v1/memory/list", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated, got %d", rec.Code)
	}
}

// Plan 1 D1 + CLAUDE.md Invariant 3: the handler MUST derive the userID
// from auth.GetUser(ctx).Subject — NEVER from a query param or body.
// Verify by setting a UserContext + an attacker-supplied query param;
// the client should receive the authenticated subject.
func TestMemoryList_DerivesUserIDFromAuthNotQuery(t *testing.T) {
	h, fake := newMemoryHandler(t)
	fake.listResp = &themory.ListResult{Memories: []themory.MemoryRow{}}

	req := authedRequest("GET", "/v1/memory/list?subject=attacker-supplied",
		&auth.UserContext{Subject: "user-A", TenantID: "tenant-A"})
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.lastUserID != "user-A" {
		t.Fatalf("client received userID=%q, want %q (D1 isolation violation if drift)", fake.lastUserID, "user-A")
	}
}

func TestMemoryList_PropagatesPaginationParams(t *testing.T) {
	h, fake := newMemoryHandler(t)
	fake.listResp = &themory.ListResult{Memories: []themory.MemoryRow{}}

	req := authedRequest("GET", "/v1/memory/list?limit=50&offset=100&category=facts",
		&auth.UserContext{Subject: "u", TenantID: "t"})
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if fake.lastOpts.Limit != 50 || fake.lastOpts.Offset != 100 || fake.lastOpts.Category != "facts" {
		t.Errorf("query params not propagated: got %+v", fake.lastOpts)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// D3: empty memories must serialise as `[]`, never `null`.
func TestMemoryList_EmptyResultSerialisesAsArrayNotNull(t *testing.T) {
	h, fake := newMemoryHandler(t)
	// Simulate TheMemory returning literally nil — the client wraps to
	// empty slice, but defense-in-depth here too.
	fake.listResp = &themory.ListResult{Memories: nil}

	rec := httptest.NewRecorder()
	h.List(rec, authedRequest("GET", "/v1/memory/list", &auth.UserContext{Subject: "u", TenantID: "t"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, `"memories":null`) {
		t.Errorf("memories MUST serialise as [], got null — D3 invariant violated. body=%s", body)
	}
	if !strings.Contains(body, `"memories":[`) {
		t.Errorf("expected memories as array, got: %s", body)
	}
}

// Client error from TheMemory → 502 Bad Gateway. Differentiated from
// 500 because the failure is in an upstream dependency, not a Go bug.
func TestMemoryList_UpstreamErrorReturns502(t *testing.T) {
	h, fake := newMemoryHandler(t)
	fake.listErr = errors.New("connection refused")

	rec := httptest.NewRecorder()
	h.List(rec, authedRequest("GET", "/v1/memory/list", &auth.UserContext{Subject: "u", TenantID: "t"}))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on upstream error, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// GET /v1/memory/metrics — T1.4
// =============================================================================

func TestMemoryMetrics_RejectsUnauthenticated(t *testing.T) {
	h, _ := newMemoryHandler(t)
	rec := httptest.NewRecorder()
	h.Metrics(rec, httptest.NewRequest("GET", "/v1/memory/metrics", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// D3: empty pgvector → zero everywhere. We simulate TheMemory returning
// the zero-initialised struct (which the client does for us); handler
// must serialise it as JSON numbers, not omit fields or send strings.
func TestMemoryMetrics_EmptyPgVectorRendersAsZeroNotNaN(t *testing.T) {
	h, fake := newMemoryHandler(t)
	fake.metricsResp = &themory.MetricsResult{} // all zeros

	rec := httptest.NewRecorder()
	h.Metrics(rec, authedRequest("GET", "/v1/memory/metrics", &auth.UserContext{Subject: "u", TenantID: "t"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v body=%s", err, rec.Body.String())
	}
	// All four metric keys MUST be present and MUST be numbers (not null,
	// not absent, not strings). This is the D3 invariant.
	for _, key := range []string{"total_memories", "retrieval_api_usage", "add_events", "entity_count"} {
		v, ok := got[key]
		if !ok {
			t.Errorf("metric %q missing from empty response (D3 violation)", key)
			continue
		}
		// JSON numbers decode to float64 in Go's untyped map.
		if _, ok := v.(float64); !ok {
			t.Errorf("metric %q has non-numeric type %T (D3 violation: %v)", key, v, v)
		}
	}
}

func TestMemoryMetrics_PopulatedEchosCorrectly(t *testing.T) {
	h, fake := newMemoryHandler(t)
	fake.metricsResp = &themory.MetricsResult{
		TotalMemories: 42, RetrievalAPIUsage: 1000, AddEvents: 5, EntityCount: 7,
	}

	rec := httptest.NewRecorder()
	h.Metrics(rec, authedRequest("GET", "/v1/memory/metrics", &auth.UserContext{Subject: "u", TenantID: "t"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"total_memories":42`) {
		t.Errorf("expected total_memories=42, got %s", rec.Body.String())
	}
}

// =============================================================================
// DELETE /v1/memory/forget/{memoryID} — T1.5
// =============================================================================

func memoryForgetReq(t *testing.T, memoryID string, u *auth.UserContext) *http.Request {
	t.Helper()
	req := httptest.NewRequest("DELETE", "/v1/memory/forget/x", nil)
	if u != nil {
		req = req.WithContext(auth.WithUser(req.Context(), u))
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("memoryID", memoryID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestMemoryForget_RejectsUnauthenticated(t *testing.T) {
	h, _ := newMemoryHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/v1/memory/forget/mem-1234", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("memoryID", "mem-1234")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	h.Forget(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// T1.5: own memory deleted OK → 204.
func TestMemoryForget_OwnMemoryReturns204(t *testing.T) {
	h, fake := newMemoryHandler(t)
	fake.forgetErr = nil

	rec := httptest.NewRecorder()
	h.Forget(rec, memoryForgetReq(t, "mem-aaaa1111bbbb2222", &auth.UserContext{Subject: "user-A", TenantID: "tenant-A"}))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.lastForget != "mem-aaaa1111bbbb2222" {
		t.Errorf("client received memoryID=%q", fake.lastForget)
	}
	if fake.lastUserID != "user-A" {
		t.Errorf("client received userID=%q, want user-A (D1 isolation violation if drift)", fake.lastUserID)
	}
}

// D5 inheritance: cross-tenant → 404 (NOT 403). TheMemory returns 404
// because the memoryID + X-LoopZero-User-Id combination doesn't match.
func TestMemoryForget_CrossTenantReturns404NotForbidden(t *testing.T) {
	h, fake := newMemoryHandler(t)
	fake.forgetErr = themory.ErrNotFound

	rec := httptest.NewRecorder()
	h.Forget(rec, memoryForgetReq(t, "mem-someoneeleses", &auth.UserContext{Subject: "user-B", TenantID: "tenant-B"}))

	if rec.Code == http.StatusForbidden {
		t.Fatal("CRITICAL: cross-tenant DELETE returned 403 — leaks memory existence (D5 violation). Must return 404.")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// EC-2-style guard: malformed memoryID rejected with 400 BEFORE the
// client is even called. Prevents path-traversal probes from polluting
// upstream logs.
func TestMemoryForget_MalformedIDRejectedBeforeClient(t *testing.T) {
	cases := []string{
		"../../etc/passwd",
		"'; DROP TABLE--",
		"", // empty
		strings.Repeat("a", 200), // too long
	}
	for _, malformed := range cases {
		t.Run(malformed, func(t *testing.T) {
			h, fake := newMemoryHandler(t)

			rec := httptest.NewRecorder()
			h.Forget(rec, memoryForgetReq(t, malformed, &auth.UserContext{Subject: "u", TenantID: "t"}))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 INVALID_ID for %q, got %d", malformed, rec.Code)
			}
			// Client must NOT have been called.
			if fake.lastForget != "" {
				t.Errorf("client called with %q before validation — malformed-id-before-client invariant violated", fake.lastForget)
			}
		})
	}
}

func TestMemoryForget_UpstreamErrorReturns502(t *testing.T) {
	h, fake := newMemoryHandler(t)
	fake.forgetErr = errors.New("connection refused")

	rec := httptest.NewRecorder()
	h.Forget(rec, memoryForgetReq(t, "mem-aaaa1111bbbb2222", &auth.UserContext{Subject: "u", TenantID: "t"}))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on upstream error, got %d", rec.Code)
	}
}

// =============================================================================
// Plan 2 — GET /v1/memory/entities — T2.3
// =============================================================================

// Extend fakeTheMemory with the Entities method. (Anonymous embedding
// would risk forgetting an interface method — explicit add.)
func (f *fakeTheMemory) Entities(_ context.Context, userID, scope string) (*themory.EntitiesResult, error) {
	f.lastUserID = userID
	f.lastEntitiesScope = scope
	return f.entitiesResp, f.entitiesErr
}

func TestMemoryEntities_RejectsUnauthenticated(t *testing.T) {
	h, _ := newMemoryHandler(t)
	rec := httptest.NewRecorder()
	h.Entities(rec, httptest.NewRequest("GET", "/v1/memory/entities?scope=user", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// T2.3 — scope param whitelist. Anything outside {user, session, agent, repo}
// is rejected 400 BEFORE the client is called.
func TestMemoryEntities_InvalidScopeReturns400(t *testing.T) {
	cases := []string{"users", "User", "USER", "../etc", "'; DROP", "", "agent;ls"}
	for _, sc := range cases {
		t.Run("scope="+sc, func(t *testing.T) {
			h, fake := newMemoryHandler(t)
			rec := httptest.NewRecorder()
			// Construct the request with a safe path; inject the
			// (possibly hostile) scope via RawQuery so httptest doesn't
			// try to parse meta-chars as part of the request line.
			req := authedRequest("GET", "/v1/memory/entities",
				&auth.UserContext{Subject: "u", TenantID: "t"})
			req.URL.RawQuery = "scope=" + url.QueryEscape(sc)
			h.Entities(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for invalid scope %q, got %d body=%s",
					sc, rec.Code, rec.Body.String())
			}
			if fake.lastEntitiesScope != "" {
				t.Errorf("client called with %q before whitelist check — gate violated", fake.lastEntitiesScope)
			}
		})
	}
}

// T2.3 — D1 + D5 inheritance: subject derived from auth, NEVER query.
func TestMemoryEntities_SubjectFromAuthNotQuery(t *testing.T) {
	h, fake := newMemoryHandler(t)
	fake.entitiesResp = &themory.EntitiesResult{Entities: []themory.Entity{}}

	rec := httptest.NewRecorder()
	req := authedRequest("GET", "/v1/memory/entities?scope=user&subject=attacker",
		&auth.UserContext{Subject: "user-A", TenantID: "tenant-A"})
	h.Entities(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.lastUserID != "user-A" {
		t.Fatalf("client received userID=%q, want user-A (D1 isolation)", fake.lastUserID)
	}
}

// T2.3 — each scope value in the whitelist resolves to a successful call.
func TestMemoryEntities_AllValidScopesAccepted(t *testing.T) {
	for _, sc := range []string{"user", "session", "agent", "repo"} {
		t.Run(sc, func(t *testing.T) {
			h, fake := newMemoryHandler(t)
			fake.entitiesResp = &themory.EntitiesResult{Entities: []themory.Entity{}}
			rec := httptest.NewRecorder()
			h.Entities(rec, authedRequest("GET", "/v1/memory/entities?scope="+sc,
				&auth.UserContext{Subject: "u", TenantID: "t"}))
			if rec.Code != http.StatusOK {
				t.Fatalf("scope=%s expected 200, got %d", sc, rec.Code)
			}
			if fake.lastEntitiesScope != sc {
				t.Errorf("client received scope=%q, want %q", fake.lastEntitiesScope, sc)
			}
		})
	}
}

// T2.3 — D3 inheritance: empty entities → `[]` not null.
func TestMemoryEntities_EmptyResultSerialisesAsArray(t *testing.T) {
	h, fake := newMemoryHandler(t)
	fake.entitiesResp = &themory.EntitiesResult{Entities: nil}

	rec := httptest.NewRecorder()
	h.Entities(rec, authedRequest("GET", "/v1/memory/entities?scope=user",
		&auth.UserContext{Subject: "u", TenantID: "t"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"entities":null`) {
		t.Errorf("D3 inheritance violated: serialised as null. body=%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"entities":[`) {
		t.Errorf("expected entities as array, got: %s", rec.Body.String())
	}
}

func TestMemoryEntities_UpstreamErrorReturns502(t *testing.T) {
	h, fake := newMemoryHandler(t)
	fake.entitiesErr = errors.New("connection refused")

	rec := httptest.NewRecorder()
	h.Entities(rec, authedRequest("GET", "/v1/memory/entities?scope=user",
		&auth.UserContext{Subject: "u", TenantID: "t"}))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}
