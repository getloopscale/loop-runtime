// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockReviewer is a programmable CrossClusterTokenReviewer for tests.
type mockReviewer struct {
	mu             sync.Mutex
	reviewCalls    int
	versionByCell  map[string]string
	resultByToken  map[string]*TokenReviewResult
	errByToken     map[string]error
	authenticated  bool
}

func newMockReviewer() *mockReviewer {
	return &mockReviewer{
		versionByCell: map[string]string{},
		resultByToken: map[string]*TokenReviewResult{},
		errByToken:    map[string]error{},
		authenticated: true,
	}
}

func (m *mockReviewer) Review(_ context.Context, cellID, token string) (*TokenReviewResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reviewCalls++
	if err, ok := m.errByToken[token]; ok {
		return nil, err
	}
	if r, ok := m.resultByToken[token]; ok {
		return r, nil
	}
	return &TokenReviewResult{
		Authenticated: m.authenticated,
		Username:      "system:serviceaccount:external-secrets:eso",
		CellID:        cellID,
	}, nil
}

func (m *mockReviewer) ClientVersion(cellID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.versionByCell[cellID]; ok {
		return v
	}
	return "v1"
}

func (m *mockReviewer) setVersion(cellID, v string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.versionByCell[cellID] = v
}

// helper to send a request through the middleware.
func sendThroughMW(t *testing.T, mw func(http.Handler) http.Handler, req *http.Request) (*httptest.ResponseRecorder, *TokenReviewResult) {
	t.Helper()
	var captured *TokenReviewResult
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r, ok := CrossClusterFromContext(r.Context()); ok {
			captured = r
		}
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()
	mw(next).ServeHTTP(rr, req)
	return rr, captured
}

func TestExtractBearerToken(t *testing.T) {
	cases := []struct {
		name    string
		header  string
		want    string
		wantErr error
	}{
		{"missing", "", "", ErrCrossClusterMissingAuthHeader},
		{"no bearer prefix", "Basic xyz", "", ErrCrossClusterBadTokenFormat},
		{"empty token", "Bearer ", "", ErrCrossClusterBadTokenFormat},
		{"valid", "Bearer abc123", "abc123", nil},
		{"trim spaces", "Bearer    abc   ", "abc", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/", nil)
			if c.header != "" {
				req.Header.Set("Authorization", c.header)
			}
			got, err := extractBearerToken(req)
			if !errors.Is(err, c.wantErr) {
				t.Errorf("err = %v, want %v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestMiddleware_HappyPath_SetsContext(t *testing.T) {
	mr := newMockReviewer()
	mw := NewCrossClusterMiddleware(CrossClusterMiddlewareConfig{Reviewer: mr})

	req, _ := http.NewRequest("POST", "/internal/v1/secret-fetch/tenant-a/MY_TOKEN", nil)
	req.Header.Set("Authorization", "Bearer good-token")
	req.Header.Set("X-Cell-ID", "cell-0")

	rr, captured := sendThroughMW(t, mw, req)
	if rr.Code != http.StatusOK {
		t.Errorf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if captured == nil {
		t.Fatal("expected captured TokenReviewResult, got nil")
	}
	if captured.CellID != "cell-0" {
		t.Errorf("CellID = %s, want cell-0", captured.CellID)
	}
	if !captured.Authenticated {
		t.Error("expected Authenticated=true")
	}
	if mr.reviewCalls != 1 {
		t.Errorf("expected 1 review call, got %d", mr.reviewCalls)
	}
}

func TestMiddleware_MissingAuthHeader_401(t *testing.T) {
	mr := newMockReviewer()
	mw := NewCrossClusterMiddleware(CrossClusterMiddlewareConfig{Reviewer: mr})

	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("X-Cell-ID", "cell-0")

	rr, captured := sendThroughMW(t, mw, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
	if captured != nil {
		t.Error("expected no context injection")
	}
}

func TestMiddleware_MissingCellHeader_401(t *testing.T) {
	mr := newMockReviewer()
	mw := NewCrossClusterMiddleware(CrossClusterMiddlewareConfig{Reviewer: mr})

	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer tok")

	rr, _ := sendThroughMW(t, mw, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "X-Cell-ID") {
		t.Errorf("body should mention X-Cell-ID, got: %s", rr.Body.String())
	}
}

func TestMiddleware_AuthenticatedFalse_401(t *testing.T) {
	mr := newMockReviewer()
	mr.authenticated = false
	mw := NewCrossClusterMiddleware(CrossClusterMiddlewareConfig{Reviewer: mr})

	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("X-Cell-ID", "cell-0")

	rr, _ := sendThroughMW(t, mw, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

func TestMiddleware_ReviewerError_401(t *testing.T) {
	mr := newMockReviewer()
	mr.errByToken = map[string]error{"badtok": errors.New("network down")}
	mw := NewCrossClusterMiddleware(CrossClusterMiddlewareConfig{Reviewer: mr})

	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer badtok")
	req.Header.Set("X-Cell-ID", "cell-0")

	rr, _ := sendThroughMW(t, mw, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

func TestMiddleware_CacheHit_SkipsRepeatReview(t *testing.T) {
	mr := newMockReviewer()
	mw := NewCrossClusterMiddleware(CrossClusterMiddlewareConfig{
		Reviewer: mr,
		CacheTTL: time.Second,
	})

	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("POST", "/", nil)
		req.Header.Set("Authorization", "Bearer cached-token")
		req.Header.Set("X-Cell-ID", "cell-0")
		rr, _ := sendThroughMW(t, mw, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("iter %d: want 200, got %d", i, rr.Code)
		}
	}
	if mr.reviewCalls != 1 {
		t.Errorf("expected exactly 1 review call (cache hits), got %d", mr.reviewCalls)
	}
}

// EC-18 critical test: ClientVersion change invalidates cache.
func TestMiddleware_EC18_KubeconfigRotationInvalidatesCache(t *testing.T) {
	mr := newMockReviewer()
	mr.versionByCell["cell-0"] = "v1"
	mw := NewCrossClusterMiddleware(CrossClusterMiddlewareConfig{
		Reviewer: mr,
		CacheTTL: 5 * time.Second,
	})

	req1, _ := http.NewRequest("POST", "/", nil)
	req1.Header.Set("Authorization", "Bearer rotated-tok")
	req1.Header.Set("X-Cell-ID", "cell-0")
	rr1, _ := sendThroughMW(t, mw, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first request: want 200, got %d", rr1.Code)
	}
	if mr.reviewCalls != 1 {
		t.Errorf("after first request, expected 1 review call, got %d", mr.reviewCalls)
	}

	// H13: kubeconfig rotation bumps the cell's client version.
	mr.setVersion("cell-0", "v2")

	req2, _ := http.NewRequest("POST", "/", nil)
	req2.Header.Set("Authorization", "Bearer rotated-tok") // SAME token
	req2.Header.Set("X-Cell-ID", "cell-0")
	rr2, _ := sendThroughMW(t, mw, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("after rotation: want 200, got %d", rr2.Code)
	}
	// Cache key changed (version embedded) → review re-runs
	if mr.reviewCalls != 2 {
		t.Errorf("after kubeconfig rotation expected 2 review calls (cache invalidated), got %d", mr.reviewCalls)
	}
}

func TestMiddleware_CacheExpiry_TriggersRevalidate(t *testing.T) {
	mr := newMockReviewer()
	mw := NewCrossClusterMiddleware(CrossClusterMiddlewareConfig{
		Reviewer: mr,
		CacheTTL: 30 * time.Millisecond,
	})

	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer expiring-tok")
	req.Header.Set("X-Cell-ID", "cell-0")

	sendThroughMW(t, mw, req)
	sendThroughMW(t, mw, req)
	if mr.reviewCalls != 1 {
		t.Errorf("within TTL: want 1 review call, got %d", mr.reviewCalls)
	}

	time.Sleep(50 * time.Millisecond)
	sendThroughMW(t, mw, req)
	if mr.reviewCalls != 2 {
		t.Errorf("after TTL: want 2 review calls, got %d", mr.reviewCalls)
	}
}

func TestNewCrossClusterMiddleware_PanicsOnNilReviewer(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil reviewer")
		}
	}()
	NewCrossClusterMiddleware(CrossClusterMiddlewareConfig{Reviewer: nil})
}

func TestCacheKey_DistinctVersionsProduceDistinctKeys(t *testing.T) {
	a := crossClusterCacheKey("cell-0", "tok", "v1")
	b := crossClusterCacheKey("cell-0", "tok", "v2")
	if a == b {
		t.Errorf("expected different keys for different versions, got identical: %s", a)
	}
}

func TestCacheKey_DistinctCellsProduceDistinctKeys(t *testing.T) {
	// As duas cells tem de ser REALMENTE distintas. Enquanto a primeira se chamou
	// `cell-default` e a segunda `cell-1`, os nomes diferiam sem que as maquinas
	// diferissem — eram o mesmo cluster (ADR 2026-08-09). Com a unificacao 1:1,
	// `cell-0` e `cell-1` sao duas cells de verdade, e a chave de cache separar as
	// duas passa a significar isolamento entre clusters, nao entre sinonimos.
	a := crossClusterCacheKey("cell-0", "tok", "v1")
	b := crossClusterCacheKey("cell-1", "tok", "v1")
	if a == b {
		t.Errorf("expected different keys for different cells")
	}
}

func TestCrossClusterFromContext_NoValue_ReturnsFalse(t *testing.T) {
	_, ok := CrossClusterFromContext(context.Background())
	if ok {
		t.Error("expected ok=false for empty context")
	}
}

func TestCustomOnFailure(t *testing.T) {
	var called bool
	mr := newMockReviewer()
	mw := NewCrossClusterMiddleware(CrossClusterMiddlewareConfig{
		Reviewer: mr,
		OnFailure: func(w http.ResponseWriter, _ error) {
			called = true
			w.WriteHeader(http.StatusTeapot)
		},
	})

	req, _ := http.NewRequest("POST", "/", nil)
	// Missing both headers → triggers OnFailure
	rr, _ := sendThroughMW(t, mw, req)
	if !called {
		t.Error("expected custom OnFailure to be called")
	}
	if rr.Code != http.StatusTeapot {
		t.Errorf("want 418, got %d", rr.Code)
	}
}
