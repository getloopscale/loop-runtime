// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// API Key Rotation plan, T6.3.
// Tests for POST /v1/api-keys/{id}/rotate.

package unit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

func rotateRequest(t *testing.T, keyID string, body string, u *auth.UserContext) *http.Request {
	t.Helper()
	var br *strings.Reader
	if body != "" {
		br = strings.NewReader(body)
	} else {
		br = strings.NewReader("{}")
	}
	req := httptest.NewRequest("POST", "/v1/api-keys/x/rotate", br)
	req.Header.Set("Content-Type", "application/json")
	if u != nil {
		req = req.WithContext(auth.WithUser(req.Context(), u))
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", keyID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestRotateKey_RejectsUnauthenticated(t *testing.T) {
	h, _ := newAPIKeysHandler(t)
	req := httptest.NewRequest("POST", "/v1/api-keys/apk-aaaa1111bbbb2222/rotate", strings.NewReader("{}"))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "apk-aaaa1111bbbb2222")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.RotateKey(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// T6.3 — happy path with default grace (24h). Old row's expires_at is set
// to NOW()+24h; new row is inserted with rotated_from = old.id. Response
// returns new key id + secret.
func TestRotateKey_HappyPathDefaultGrace(t *testing.T) {
	h, mock := newAPIKeysHandler(t)

	mock.ExpectBegin()
	// SELECT FOR UPDATE returns the old key's name + scopes + expires_at.
	mock.ExpectQuery("SELECT key_name, scopes, revoked_at, expires_at FROM loopzero_api_keys").
		WithArgs("apk-0d11111111aaaaaa", "user-A", "tenant-A").
		WillReturnRows(sqlmock.NewRows([]string{"key_name", "scopes", "revoked_at", "expires_at"}).
			AddRow("prod-agent", []byte(`["memory:read","memory:write"]`), nil, nil))
	// INSERT new row.
	mock.ExpectExec("INSERT INTO loopzero_api_keys").
		WithArgs(
			sqlmock.AnyArg(), // new id apk-{16hex}
			"user-A", "tenant-A",
			sqlmock.AnyArg(), // new name = old + " (rotated YYYY-MM-DD)"
			sqlmock.AnyArg(), // key_hash
			sqlmock.AnyArg(), // key_prefix
			sqlmock.AnyArg(), // scopes
			sqlmock.AnyArg(), // expires_at (nil)
			"apk-0d11111111aaaaaa", // rotated_from
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	// UPDATE old row's expires_at.
	mock.ExpectExec("UPDATE loopzero_api_keys SET expires_at").
		WithArgs(sqlmock.AnyArg(), "apk-0d11111111aaaaaa").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	rec := httptest.NewRecorder()
	h.RotateKey(rec, rotateRequest(t, "apk-0d11111111aaaaaa", "", &auth.UserContext{Subject: "user-A", TenantID: "tenant-A"}))

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
	if !strings.HasPrefix(resp.ID, "apk-") {
		t.Errorf("new id must start with apk-, got %q", resp.ID)
	}
	if !strings.HasPrefix(resp.Secret, "loopzero_live_") {
		t.Errorf("new secret must start with loopzero_live_, got %q", resp.Secret)
	}
}

// T6.3 — grace_window_hours body param accepted within 1-168.
func TestRotateKey_AcceptsValidGraceWindowHours(t *testing.T) {
	cases := []int{1, 24, 72, 168}
	for _, hours := range cases {
		t.Run(time.Duration(hours).String(), func(t *testing.T) {
			h, mock := newAPIKeysHandler(t)
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT key_name, scopes, revoked_at, expires_at").
				WithArgs("apk-0d11111111aaaaaa", "u", "t").
				WillReturnRows(sqlmock.NewRows([]string{"key_name", "scopes", "revoked_at", "expires_at"}).
					AddRow("k", []byte(`["memory:read"]`), nil, nil))
			mock.ExpectExec("INSERT INTO loopzero_api_keys").WithArgs(
				sqlmock.AnyArg(), "u", "t", sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "apk-0d11111111aaaaaa",
			).WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("UPDATE loopzero_api_keys SET expires_at").
				WithArgs(sqlmock.AnyArg(), "apk-0d11111111aaaaaa").
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()

			body := `{"grace_window_hours":` + intToStr(hours) + `}`
			rec := httptest.NewRecorder()
			h.RotateKey(rec, rotateRequest(t, "apk-0d11111111aaaaaa", body, &auth.UserContext{Subject: "u", TenantID: "t"}))
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 for hours=%d, got %d body=%s", hours, rec.Code, rec.Body.String())
			}
		})
	}
}

// T6.3 — out-of-range grace_window_hours → 400 INVALID_GRACE_WINDOW.
func TestRotateKey_RejectsOutOfRangeGraceWindow(t *testing.T) {
	cases := []int{0, -1, 169, 9999}
	for _, hours := range cases {
		t.Run(intToStr(hours), func(t *testing.T) {
			h, _ := newAPIKeysHandler(t)
			body := `{"grace_window_hours":` + intToStr(hours) + `}`
			rec := httptest.NewRecorder()
			h.RotateKey(rec, rotateRequest(t, "apk-0d11111111aaaaaa", body, &auth.UserContext{Subject: "u", TenantID: "t"}))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for hours=%d, got %d", hours, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "INVALID_GRACE_WINDOW") {
				t.Errorf("expected INVALID_GRACE_WINDOW error code, got %s", rec.Body.String())
			}
		})
	}
}

// T6.3 — Cross-tenant rotation → 404 NOT 403 (D5 P0 invariant).
func TestRotateKey_CrossTenantReturns404NotForbidden(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT key_name, scopes, revoked_at, expires_at").
		WithArgs("apk-50fea50fea50fea5", "user-B", "tenant-B").
		WillReturnError(sqlNoRowsErr())
	mock.ExpectRollback()

	rec := httptest.NewRecorder()
	h.RotateKey(rec, rotateRequest(t, "apk-50fea50fea50fea5", "", &auth.UserContext{Subject: "user-B", TenantID: "tenant-B"}))

	if rec.Code == http.StatusForbidden {
		t.Fatal("CRITICAL: cross-tenant rotation returned 403 — leaks existence (D5 violation)")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant, got %d", rec.Code)
	}
}

// T6.3 — Already-revoked key cannot be rotated → 404.
func TestRotateKey_AlreadyRevokedReturns404(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	mock.ExpectBegin()
	now := time.Now()
	mock.ExpectQuery("SELECT key_name, scopes, revoked_at, expires_at").
		WithArgs("apk-0e0111111111aaaa", "u", "t").
		WillReturnRows(sqlmock.NewRows([]string{"key_name", "scopes", "revoked_at", "expires_at"}).
			AddRow("k", []byte(`["memory:read"]`), &now, nil))
	mock.ExpectRollback()

	rec := httptest.NewRecorder()
	h.RotateKey(rec, rotateRequest(t, "apk-0e0111111111aaaa", "", &auth.UserContext{Subject: "u", TenantID: "t"}))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for revoked, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// T6.3 — Already-expired key (expires_at in the past) → 404.
func TestRotateKey_AlreadyExpiredReturns404(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	mock.ExpectBegin()
	past := time.Now().Add(-1 * time.Hour)
	mock.ExpectQuery("SELECT key_name, scopes, revoked_at, expires_at").
		WithArgs("apk-e051111111111aaa", "u", "t").
		WillReturnRows(sqlmock.NewRows([]string{"key_name", "scopes", "revoked_at", "expires_at"}).
			AddRow("k", []byte(`["memory:read"]`), nil, &past))
	mock.ExpectRollback()

	rec := httptest.NewRecorder()
	h.RotateKey(rec, rotateRequest(t, "apk-e051111111111aaa", "", &auth.UserContext{Subject: "u", TenantID: "t"}))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for already-expired, got %d", rec.Code)
	}
}

// T6.3 — Malformed id rejected with 400 BEFORE DB query (EC-2 pattern from P0).
func TestRotateKey_MalformedIDRejectedBeforeDB(t *testing.T) {
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
			h.RotateKey(rec, rotateRequest(t, malformed, "", &auth.UserContext{Subject: "u", TenantID: "t"}))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 INVALID_ID for %q, got %d", malformed, rec.Code)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("DB called before validation — gate violated: %v", err)
			}
		})
	}
}

// Helper.
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}
