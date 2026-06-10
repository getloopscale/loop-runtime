// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Plan 3 of dashboard-memory-section-roadmap.md, T3.4.
// Tests for the 3 memory webhook handlers (Create / List / Delete).
// All gated by RequireScope("memory:admin") at registration; here we test
// the handler-level invariants: subject-from-auth, URL validation,
// cross-tenant 404, soft-delete semantics.

package unit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	"github.com/loopzero/loopzero/cloud/internal/routes"
)

func newWebhooksHandler(t *testing.T) (*routes.MemoryWebhooksHandler, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return routes.NewMemoryWebhooksHandler(db, nil), mock
}

func TestMemoryWebhooks_CreateRejectsUnauthenticated(t *testing.T) {
	h, _ := newWebhooksHandler(t)
	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest("POST", "/v1/memory/webhooks", strings.NewReader(`{}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestMemoryWebhooks_CreateHappyPath(t *testing.T) {
	h, mock := newWebhooksHandler(t)
	mock.ExpectExec("INSERT INTO memory_webhooks").
		WithArgs(
			sqlmock.AnyArg(), // id (mwh-prefixed)
			"user-A", "tenant-A",
			"https://hook.example.com/foo",
			"prod-events",
			sqlmock.AnyArg(), // events jsonb
			sqlmock.AnyArg(), // secret (Memory Webhook Delivery plan D1)
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body := `{"url":"https://hook.example.com/foo","label":"prod-events","events":["memory.remembered"]}`
	req := httptest.NewRequest("POST", "/v1/memory/webhooks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.WithUser(req.Context(), &auth.UserContext{Subject: "user-A", TenantID: "tenant-A"}))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMemoryWebhooks_CreateRejectsInvalidURL(t *testing.T) {
	cases := []string{
		"ftp://hook.example.com",
		"javascript:alert(1)",
		"file:///etc/passwd",
		"",
		"not-a-url",
	}
	for _, bad := range cases {
		t.Run(bad, func(t *testing.T) {
			h, _ := newWebhooksHandler(t)
			body := `{"url":"` + bad + `","label":"x","events":["memory.remembered"]}`
			req := httptest.NewRequest("POST", "/v1/memory/webhooks", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(auth.WithUser(req.Context(), &auth.UserContext{Subject: "u", TenantID: "t"}))
			rec := httptest.NewRecorder()
			h.Create(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for url %q, got %d body=%s", bad, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestMemoryWebhooks_CreateRejectsEmptyEvents(t *testing.T) {
	h, _ := newWebhooksHandler(t)
	body := `{"url":"https://hook.example.com","label":"x","events":[]}`
	req := httptest.NewRequest("POST", "/v1/memory/webhooks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.WithUser(req.Context(), &auth.UserContext{Subject: "u", TenantID: "t"}))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty events, got %d", rec.Code)
	}
}

func TestMemoryWebhooks_ListReturnsArrayFilteredByTenant(t *testing.T) {
	h, mock := newWebhooksHandler(t)
	mock.ExpectQuery("SELECT id, url, label, events, created_at FROM memory_webhooks").
		WithArgs("user-A", "tenant-A").
		WillReturnRows(sqlmock.NewRows([]string{"id", "url", "label", "events", "created_at"}))

	req := httptest.NewRequest("GET", "/v1/memory/webhooks/list", nil)
	req = req.WithContext(auth.WithUser(req.Context(), &auth.UserContext{Subject: "user-A", TenantID: "tenant-A"}))
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"webhooks":null`) {
		t.Errorf("D3 inheritance — webhooks must serialise as [], not null")
	}
}

// Cross-tenant DELETE → 404 (D5 inheritance from P0).
func TestMemoryWebhooks_CrossTenantDeleteReturns404(t *testing.T) {
	h, mock := newWebhooksHandler(t)
	mock.ExpectExec("UPDATE memory_webhooks SET revoked_at").
		WithArgs("mwh-aaaa1111bbbb2222", "user-B", "tenant-B").
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 affected — wrong tenant

	req := httptest.NewRequest("DELETE", "/v1/memory/webhooks/x", nil)
	req = req.WithContext(auth.WithUser(req.Context(), &auth.UserContext{Subject: "user-B", TenantID: "tenant-B"}))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "mwh-aaaa1111bbbb2222")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.Delete(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatal("CRITICAL: cross-tenant DELETE returned 403 — D5 violation; must return 404")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant, got %d", rec.Code)
	}
}

// =============================================================================
// Memory Webhook Delivery plan — T5.4
// =============================================================================

// T5.4 — Create handler MUST generate a per-webhook secret, persist it raw
// via INSERT, and return it ONCE in the response (Stripe show-once pattern).
// Tests: format regex (whsec_<43 base64url>); persisted via INSERT; unique.
func TestMemoryWebhooks_CreateGeneratesAndReturnsSecret(t *testing.T) {
	h, mock := newWebhooksHandler(t)
	mock.ExpectExec("INSERT INTO memory_webhooks").
		WithArgs(
			sqlmock.AnyArg(), "user-A", "tenant-A",
			"https://hook.example.com/foo",
			"prod-events",
			sqlmock.AnyArg(), // events jsonb
			sqlmock.AnyArg(), // secret — must be the persisted raw secret
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body := `{"url":"https://hook.example.com/foo","label":"prod-events","events":["memory.forgotten"]}`
	req := httptest.NewRequest("POST", "/v1/memory/webhooks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.WithUser(req.Context(), &auth.UserContext{Subject: "user-A", TenantID: "tenant-A"}))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID     string `json:"id"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v body=%s", err, rec.Body.String())
	}
	// D1 invariant: secret format whsec_<43 base64url chars>
	if !strings.HasPrefix(resp.Secret, "whsec_") {
		t.Errorf("secret must start with whsec_, got %q", resp.Secret)
	}
	// 32 bytes base64url RawURLEncoding = 43 chars; whsec_ (6) + 43 = 49 total.
	if len(resp.Secret) != 49 {
		t.Errorf("secret total length must be 49 (whsec_ + 43 base64url), got %d", len(resp.Secret))
	}
}

// T5.4 — two consecutive creates produce DIFFERENT secrets (random, not constant).
func TestMemoryWebhooks_CreateProducesUniqueSecrets(t *testing.T) {
	h, mock := newWebhooksHandler(t)
	mock.ExpectExec("INSERT INTO memory_webhooks").
		WithArgs(sqlmock.AnyArg(), "u", "t", "https://x", "a", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO memory_webhooks").
		WithArgs(sqlmock.AnyArg(), "u", "t", "https://x", "b", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	doCreate := func(label string) string {
		body := `{"url":"https://x","label":"` + label + `","events":["memory.forgotten"]}`
		req := httptest.NewRequest("POST", "/v1/memory/webhooks", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(auth.WithUser(req.Context(), &auth.UserContext{Subject: "u", TenantID: "t"}))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		var resp struct{ Secret string `json:"secret"` }
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp.Secret
	}
	s1 := doCreate("a")
	s2 := doCreate("b")
	if s1 == s2 {
		t.Errorf("two creates produced identical secrets (RNG broken or hardcoded): %q == %q", s1, s2)
	}
}

func TestMemoryWebhooks_DeleteMalformedIDRejectedBeforeDB(t *testing.T) {
	cases := []string{"'; DROP", "../etc", "mwh-XYZ", "mwh-", ""}
	for _, malformed := range cases {
		t.Run(malformed, func(t *testing.T) {
			h, mock := newWebhooksHandler(t)
			req := httptest.NewRequest("DELETE", "/v1/memory/webhooks/x", nil)
			req = req.WithContext(auth.WithUser(req.Context(), &auth.UserContext{Subject: "u", TenantID: "t"}))
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", malformed)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			rec := httptest.NewRecorder()
			h.Delete(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 INVALID_ID, got %d", rec.Code)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("DB called before validation — gate violated: %v", err)
			}
		})
	}
}
