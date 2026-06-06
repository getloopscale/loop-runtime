// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// API Key Lineage View plan, T7.2.
// Tests for GET /v1/api-keys/{id}/lineage.

package unit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

func lineageRequest(t *testing.T, keyID string, u *auth.UserContext) *http.Request {
	t.Helper()
	req := httptest.NewRequest("GET", "/v1/api-keys/x/lineage", nil)
	if u != nil {
		req = req.WithContext(auth.WithUser(req.Context(), u))
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", keyID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestLineageKey_RejectsUnauthenticated(t *testing.T) {
	h, _ := newAPIKeysHandler(t)
	req := httptest.NewRequest("GET", "/v1/api-keys/apk-aaaa1111bbbb2222/lineage", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "apk-aaaa1111bbbb2222")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.LineageKey(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// T7.2 — Malformed id rejected BEFORE DB (EC-2 pattern from P0).
func TestLineageKey_MalformedIDRejectedBeforeDB(t *testing.T) {
	cases := []string{
		"../etc/passwd",
		"'; DROP",
		"not-a-key",
		"apk-",
		"",
		"apk-XYZ",
	}
	for _, malformed := range cases {
		t.Run(malformed, func(t *testing.T) {
			h, mock := newAPIKeysHandler(t)
			rec := httptest.NewRecorder()
			h.LineageKey(rec, lineageRequest(t, malformed, &auth.UserContext{Subject: "u", TenantID: "t"}))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %q, got %d", malformed, rec.Code)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("DB called before validation: %v", err)
			}
		})
	}
}

// T7.2 — Cross-tenant id → 404 (D3 + P0 D5 invariant). The CTE returns zero
// rows when the (subject, tenant_id) filter doesn't match.
func TestLineageKey_CrossTenantReturns404(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	mock.ExpectQuery("WITH RECURSIVE").
		WithArgs("apk-50fea50fea50fea5", "user-B", "tenant-B").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "key_name", "key_prefix", "created_at", "revoked_at", "expires_at",
		}))

	rec := httptest.NewRecorder()
	h.LineageKey(rec, lineageRequest(t, "apk-50fea50fea50fea5", &auth.UserContext{Subject: "user-B", TenantID: "tenant-B"}))

	if rec.Code == http.StatusForbidden {
		t.Fatal("CRITICAL: cross-tenant lineage returned 403 — D5 violation")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// T7.2 — Single-key (no rotation history) returns 1-item lineage with
// is_current=true.
func TestLineageKey_SingleKeyReturnsOneItem(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	now := time.Now()
	mock.ExpectQuery("WITH RECURSIVE").
		WithArgs("apk-aaaa1111bbbb2222", "u", "t").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "key_name", "key_prefix", "created_at", "revoked_at", "expires_at",
		}).AddRow("apk-aaaa1111bbbb2222", "solo-key", "loopzero_live_So", now, nil, nil))

	rec := httptest.NewRecorder()
	h.LineageKey(rec, lineageRequest(t, "apk-aaaa1111bbbb2222", &auth.UserContext{Subject: "u", TenantID: "t"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Lineage []map[string]any `json:"lineage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp.Lineage) != 1 {
		t.Fatalf("expected 1 lineage item, got %d", len(resp.Lineage))
	}
	if resp.Lineage[0]["is_current"] != true {
		t.Errorf("solo key MUST be is_current=true, got %v", resp.Lineage[0]["is_current"])
	}
}

// T7.2 — Multi-rotation chain returns N+1 items ordered root → current,
// with exactly ONE item flagged is_current.
func TestLineageKey_MultiRotationChainOrdered(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	t1 := time.Now().Add(-72 * time.Hour)
	t2 := time.Now().Add(-48 * time.Hour)
	t3 := time.Now().Add(-24 * time.Hour)
	t4 := time.Now()
	// Recursive CTE result: 4 rows representing root → r1 → r2 → current.
	// Server-side ORDER BY created_at ASC gives root-first.
	mock.ExpectQuery("WITH RECURSIVE").
		WithArgs("apk-1211121112111211"[:20], "u", "t").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "key_name", "key_prefix", "created_at", "revoked_at", "expires_at",
		}).
			AddRow("apk-10aaaaaaaaaaaaaa", "prod-agent (root)", "loopzero_live_Aa", t1, nil, &t2). // root, expired at t2 (rotated)
			AddRow("apk-11aaaaaaaaaaaaaa", "prod-agent (rotated 2026-06-04)", "loopzero_live_Bb", t2, nil, &t3). // r1, expired at t3
			AddRow("apk-12aaaaaaaaaaaaaa", "prod-agent (rotated 2026-06-05)", "loopzero_live_Cc", t3, nil, &t4). // r2, expired at t4
			AddRow("apk-c00ec00ec00ec00e", "prod-agent (rotated 2026-06-06)", "loopzero_live_Dd", t4, nil, nil)) // current

	rec := httptest.NewRecorder()
	h.LineageKey(rec, lineageRequest(t, "apk-1211121112111211"[:20], &auth.UserContext{Subject: "u", TenantID: "t"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Lineage []struct {
			ID        string `json:"id"`
			IsCurrent bool   `json:"is_current"`
		} `json:"lineage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp.Lineage) != 4 {
		t.Fatalf("expected 4 lineage items, got %d", len(resp.Lineage))
	}
	// Root first (oldest), current last per D1.
	if resp.Lineage[0].ID != "apk-10aaaaaaaaaaaaaa" {
		t.Errorf("expected root first, got %q", resp.Lineage[0].ID)
	}
	if resp.Lineage[3].ID != "apk-c00ec00ec00ec00e" {
		t.Errorf("expected current last, got %q", resp.Lineage[3].ID)
	}
	// Exactly one is_current=true and it's the last.
	currentCount := 0
	for _, item := range resp.Lineage {
		if item.IsCurrent {
			currentCount++
		}
	}
	if currentCount != 1 {
		t.Errorf("expected exactly one is_current=true item, got %d", currentCount)
	}
	if !resp.Lineage[3].IsCurrent {
		t.Errorf("last item MUST be is_current=true")
	}
}

// T7.2 — DB error → 500.
func TestLineageKey_DBErrorReturns500(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	mock.ExpectQuery("WITH RECURSIVE").
		WithArgs("apk-aaaa1111bbbb2222", "u", "t").
		WillReturnError(errors.New("connection closed"))

	rec := httptest.NewRecorder()
	h.LineageKey(rec, lineageRequest(t, "apk-aaaa1111bbbb2222", &auth.UserContext{Subject: "u", TenantID: "t"}))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on DB error, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "INTERNAL") {
		t.Errorf("expected INTERNAL code, got %s", rec.Body.String())
	}
}
