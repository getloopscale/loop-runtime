// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Plan 0 of dashboard-memory-section-roadmap.md, T0.4.
// Tests for DELETE /v1/api-keys/{id} — soft-delete handler per D7,
// with D5 cross-tenant invariant (404 not 403) and EC-2 ID-format
// validation BEFORE DB query.

package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

// chiRequestWithID returns a request whose chi.URLParam("id") yields keyID.
// chi stores route params on a context value — to populate it inside a
// unit test we manually attach a *chi.Context. This mirrors how
// chi.URLParam fetches params at request time.
func chiRequestWithID(method, target, keyID string, u *auth.UserContext) *http.Request {
	req := authedRequest(method, target, u)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", keyID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestRevokeKey_RejectsUnauthenticated(t *testing.T) {
	h, _ := newAPIKeysHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/v1/api-keys/apk-abcd1234abcd1234", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "apk-abcd1234abcd1234")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	h.RevokeKey(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// T0.4 case 1: own key revoked OK.
// UPDATE returns RowsAffected=1 → response 204 No Content.
func TestRevokeKey_OwnKeyRevoked(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	mock.ExpectExec("UPDATE loopzero_api_keys SET revoked_at").
		WithArgs("apk-abcd1234abcd1234", "user-A", "tenant-A").
		WillReturnResult(sqlmock.NewResult(0, 1))

	rec := httptest.NewRecorder()
	req := chiRequestWithID("DELETE", "/v1/api-keys/apk-abcd1234abcd1234", "apk-abcd1234abcd1234",
		&auth.UserContext{Subject: "user-A", TenantID: "tenant-A"})
	h.RevokeKey(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("UPDATE did not run as expected: %v", err)
	}
}

// T0.4 case 2 (D5 invariant): cross-tenant DELETE returns 404 — NOT 403.
// Returning 403 would leak existence ("key exists but you can't access it");
// 404 is indistinguishable from "key does not exist anywhere", which
// is the existence-leak prevention required by the plan's D5 ADR.
// UPDATE returns RowsAffected=0 because the WHERE clause includes
// tenant_id = $3, so the cross-tenant attacker's row was never touched.
func TestRevokeKey_CrossTenantReturns404NotForbidden(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	mock.ExpectExec("UPDATE loopzero_api_keys SET revoked_at").
		WithArgs("apk-abcd1234abcd1234", "user-B", "tenant-B").
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 affected — key belongs to a different tenant

	rec := httptest.NewRecorder()
	req := chiRequestWithID("DELETE", "/v1/api-keys/apk-abcd1234abcd1234", "apk-abcd1234abcd1234",
		&auth.UserContext{Subject: "user-B", TenantID: "tenant-B"})
	h.RevokeKey(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatal("CRITICAL: cross-tenant DELETE returned 403 — leaks key existence (D5 violation). Must return 404.")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// T0.4 case 3: re-revoking already-revoked key returns 404 (idempotent from
// user perspective). The WHERE clause includes `revoked_at IS NULL`, so a
// second DELETE finds 0 rows and is indistinguishable from non-existent.
func TestRevokeKey_AlreadyRevokedReturns404(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	mock.ExpectExec("UPDATE loopzero_api_keys SET revoked_at").
		WithArgs("apk-abcd1234abcd1234", "user-A", "tenant-A").
		WillReturnResult(sqlmock.NewResult(0, 0))

	rec := httptest.NewRecorder()
	req := chiRequestWithID("DELETE", "/v1/api-keys/apk-abcd1234abcd1234", "apk-abcd1234abcd1234",
		&auth.UserContext{Subject: "user-A", TenantID: "tenant-A"})
	h.RevokeKey(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for already-revoked, got %d", rec.Code)
	}
}

// T0.4 case 4 — EC-2 absorption: malformed ID rejected with 400 BEFORE
// any DB query. The plan calls this "ULID format validation" but the
// codebase convention is `apk-{16hex}` per pkg/id (ADR-0028). The security
// invariant is identical: validate-format-before-query.
// We assert via sqlmock that NO query is invoked — a panic or unexpected
// SQL means the gate was bypassed.
func TestRevokeKey_MalformedIDRejectedBeforeDB(t *testing.T) {
	cases := []string{
		"../../../etc/passwd",
		"'; DROP TABLE--",
		"not-a-key-id",
		"apk-XYZ", // wrong character set
		"apk-",    // empty body
		"",        // empty entirely
		"apk-abcd1234abcd123",  // 15 hex chars — off by one
		"apk-abcd1234abcd12345", // 17 hex chars — off by one
	}
	for _, malformed := range cases {
		t.Run(malformed, func(t *testing.T) {
			h, mock := newAPIKeysHandler(t)
			// CRITICAL: zero query expectations registered. If the handler
			// reaches the DB before validating, ExpectationsWereMet will
			// catch an unexpected call OR the missing-expectation error.
			rec := httptest.NewRecorder()
			// URL path is irrelevant — the handler reads `id` from
			// chi.URLParam, which we control via the route context. Using
			// a safe placeholder URL keeps httptest.NewRequest from
			// mis-parsing meta-chars like `'; DROP TABLE--`.
			req := chiRequestWithID("DELETE", "/v1/api-keys/x", malformed,
				&auth.UserContext{Subject: "user-A", TenantID: "tenant-A"})
			h.RevokeKey(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 INVALID_ID for malformed %q, got %d body=%s",
					malformed, rec.Code, rec.Body.String())
			}
			// Verify no DB query was emitted — sqlmock would fail if one was.
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unexpected DB activity for malformed ID %q: %v", malformed, err)
			}
		})
	}
}
