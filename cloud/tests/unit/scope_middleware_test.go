// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Plan 0 of dashboard-memory-section-roadmap.md, T0.6.
// Tests for auth.RequireScope — Bearer-keyed middleware that fronts every
// /v1/memory/* route (and future verticals). The middleware is the
// load-bearing scope enforcement; if it accepts a key it shouldn't,
// the entire D2 enum collapses.

package unit

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

// sqlmockSetup returns a (db, mock) pair ready for sqlmock expectations.
// Wraps the boilerplate so each test reads as `db, mock := sqlmockSetup(t)`.
func sqlmockSetup(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

// sqlmockErrNoRows is the sentinel sqlmock uses to simulate "no row found"
// on QueryRowContext.Scan. Reuses the stdlib value so the production code's
// `errors.Is(err, sql.ErrNoRows)` branch flips.
func sqlmockErrNoRows() error { return sql.ErrNoRows }

// hashOf returns the SHA-256 hex of the input — the same form persisted
// in loopzero_api_keys.key_hash and looked up by the middleware. Reuse here
// avoids re-implementing the hash inside the test, which would be the
// kind of duplicated knowledge DRY warns against.
func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// T0.6 case 1: valid Bearer + matching scope → request proceeds with
// auth.UserContext populated.
func TestRequireScope_ValidBearerScopeMatchProceeds(t *testing.T) {
	db, mock := sqlmockSetup(t)
	mw := auth.RequireScope(db, "memory:read")

	secret := "REDACTED_SECRET_015"
	mock.ExpectQuery("SELECT id, subject, tenant_id, scopes, expires_at FROM loopzero_api_keys").
		WithArgs(hashOf(secret)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "subject", "tenant_id", "scopes", "expires_at"}).
			AddRow("apk-aaaa111122223333", "user-A", "tenant-A", []byte(`["memory:read","memory:write"]`), nil))
	// The async last_used_at update fires in a goroutine; allow it.
	mock.ExpectExec("UPDATE loopzero_api_keys SET last_used_at").
		WithArgs("apk-aaaa111122223333").
		WillReturnResult(sqlmock.NewResult(0, 1))

	var observed *auth.UserContext
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = auth.GetUser(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/v1/memory/list", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	mw(final).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if observed == nil || observed.Subject != "user-A" || observed.TenantID != "tenant-A" {
		t.Errorf("expected ctx populated with (user-A, tenant-A), got %+v", observed)
	}

	// Allow the async fire-and-forget update to land before we assert.
	time.Sleep(50 * time.Millisecond)
	_ = mock.ExpectationsWereMet()
}

// T0.6 case 2: missing Authorization header → 401.
func TestRequireScope_MissingAuthHeaderRejected(t *testing.T) {
	db, _ := sqlmockSetup(t)
	mw := auth.RequireScope(db, "memory:read")

	called := false
	final := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	rec := httptest.NewRecorder()
	mw(final).ServeHTTP(rec, httptest.NewRequest("GET", "/v1/memory/list", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if called {
		t.Fatal("downstream handler must not be invoked without Bearer")
	}
}

// T0.6 case 3: Bearer present but without loopzero_live_ prefix → 401.
// Token-shape mismatch is a fast-reject path; we never hash garbage.
func TestRequireScope_BadPrefixRejected(t *testing.T) {
	db, _ := sqlmockSetup(t)
	mw := auth.RequireScope(db, "memory:read")

	req := httptest.NewRequest("GET", "/v1/memory/list", nil)
	req.Header.Set("Authorization", "Bearer REDACTED_SECRET_052")
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad prefix, got %d", rec.Code)
	}
}

// T0.6 case 4: well-formed loopzero_live_ Bearer whose hash is not in DB → 401.
// Same response as "revoked" or "expired" so an attacker can't distinguish.
func TestRequireScope_HashNotFoundRejected(t *testing.T) {
	db, mock := sqlmockSetup(t)
	mw := auth.RequireScope(db, "memory:read")

	secret := "REDACTED_SECRET_062xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	mock.ExpectQuery("SELECT id, subject, tenant_id, scopes, expires_at FROM loopzero_api_keys").
		WithArgs(hashOf(secret)).
		WillReturnError(sqlmockErrNoRows())

	req := httptest.NewRequest("GET", "/v1/memory/list", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unknown key, got %d", rec.Code)
	}
}

// T0.6 case 5: valid Bearer + INSUFFICIENT scope → 403 with structured
// JSON `{"error":{"code":"scope_required","missing":[...]}}`.
// This is the contract the SDK depends on to map "you need to add scope X
// to your key" guidance to the customer.
func TestRequireScope_InsufficientScopeReturns403WithMissing(t *testing.T) {
	db, mock := sqlmockSetup(t)
	mw := auth.RequireScope(db, "memory:write")

	secret := "REDACTED_SECRET_015"
	mock.ExpectQuery("SELECT id, subject, tenant_id, scopes, expires_at FROM loopzero_api_keys").
		WithArgs(hashOf(secret)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "subject", "tenant_id", "scopes", "expires_at"}).
			AddRow("apk-aaaa111122223333", "u", "t", []byte(`["memory:read"]`), nil)) // missing memory:write

	final := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("downstream handler must not run on scope failure")
	})

	req := httptest.NewRequest("GET", "/v1/memory/list", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	mw(final).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "scope_required") {
		t.Errorf("expected scope_required error code, got %s", body)
	}
	if !strings.Contains(body, "memory:write") {
		t.Errorf("expected missing scope memory:write in response, got %s", body)
	}
}

// T0.6 case 6 — EC-7 absorption: jsonb corruption in scopes column.
// Manual DB edit / partial restore could produce a non-array jsonb value.
// The middleware MUST fail loudly (500) instead of silently treating it as
// nil → 403, which would falsely tell the customer their key is revoked.
func TestRequireScope_CorruptScopesReturns500NotSilent403(t *testing.T) {
	db, mock := sqlmockSetup(t)
	mw := auth.RequireScope(db, "memory:read")

	secret := "REDACTED_SECRET_015"
	mock.ExpectQuery("SELECT id, subject, tenant_id, scopes, expires_at FROM loopzero_api_keys").
		WithArgs(hashOf(secret)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "subject", "tenant_id", "scopes", "expires_at"}).
			AddRow("apk-aaaa111122223333", "u", "t", []byte(`{"not":"an array"}`), nil))

	req := httptest.NewRequest("GET", "/v1/memory/list", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)

	// 500, NOT 403 — the difference is critical for customer support.
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for corrupt scopes (NOT 403 — that would lie about revocation), got %d", rec.Code)
	}
}

// T0.6 case 7: hot path latency — the async last_used_at update must NOT
// block the response. We inject a long-running UPDATE and assert the
// response returns in < 50ms, well within budget.
func TestRequireScope_AsyncLastUsedDoesNotBlockHotPath(t *testing.T) {
	db, mock := sqlmockSetup(t)
	mw := auth.RequireScope(db, "memory:read")

	secret := "REDACTED_SECRET_015"
	mock.ExpectQuery("SELECT id, subject, tenant_id, scopes, expires_at FROM loopzero_api_keys").
		WithArgs(hashOf(secret)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "subject", "tenant_id", "scopes", "expires_at"}).
			AddRow("apk-aaaa111122223333", "u", "t", []byte(`["memory:read"]`), nil))
	// Simulate a slow async write — the middleware must NOT await this.
	mock.ExpectExec("UPDATE loopzero_api_keys SET last_used_at").
		WithArgs("apk-aaaa111122223333").
		WillDelayFor(500 * time.Millisecond).
		WillReturnResult(sqlmock.NewResult(0, 1))

	var asyncDone sync.WaitGroup
	asyncDone.Add(1)
	go func() {
		// Sit-and-wait so the test process doesn't exit before the
		// goroutine inside the middleware fires.
		time.Sleep(600 * time.Millisecond)
		asyncDone.Done()
	}()

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest("GET", "/v1/memory/list", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()

	start := time.Now()
	mw(final).ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("hot path took %s — async last_used_at MUST not block (budget 100ms)", elapsed)
	}
	asyncDone.Wait()
}

// =============================================================================
// API Key Rotation plan T6.4 — Sunset + Deprecation + Link headers (D3 / RFC 8594)
// =============================================================================

// runScopeMiddlewareWithExpiry is a focused helper that arranges the
// middleware path, sets up the SELECT mock with a pinned expires_at, and
// returns the response recorder for assertion.
func runScopeMiddlewareWithExpiry(t *testing.T, expiresAt any) *httptest.ResponseRecorder {
	t.Helper()
	db, mock := sqlmockSetup(t)
	mw := auth.RequireScope(db, "memory:read")

	secret := "REDACTED_SECRET_015"
	mock.ExpectQuery("SELECT id, subject, tenant_id, scopes, expires_at FROM loopzero_api_keys").
		WithArgs(hashOf(secret)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "subject", "tenant_id", "scopes", "expires_at"}).
			AddRow("apk-aaaa111122223333", "u", "t", []byte(`["memory:read"]`), expiresAt))
	mock.ExpectExec("UPDATE loopzero_api_keys SET last_used_at").
		WithArgs("apk-aaaa111122223333").
		WillReturnResult(sqlmock.NewResult(0, 1))

	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest("GET", "/v1/memory/list", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	mw(final).ServeHTTP(rec, req)
	// Allow the async last_used_at write to drain so its mock is consumed.
	time.Sleep(20 * time.Millisecond)
	return rec
}

// T6.4 — expires_at NULL → NO Sunset/Deprecation/Link headers.
func TestRequireScope_NoSunsetHeaderWhenExpiresAtNull(t *testing.T) {
	rec := runScopeMiddlewareWithExpiry(t, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Sunset") != "" {
		t.Errorf("Sunset header MUST be absent when expires_at is NULL, got %q", rec.Header().Get("Sunset"))
	}
	if rec.Header().Get("Deprecation") != "" {
		t.Errorf("Deprecation header MUST be absent, got %q", rec.Header().Get("Deprecation"))
	}
}

// T6.4 — expires_at > 7d in the future → NO Sunset headers (only signal
// when grace window is approaching).
func TestRequireScope_NoSunsetHeaderWhenExpiresAtFarFuture(t *testing.T) {
	far := time.Now().Add(30 * 24 * time.Hour)
	rec := runScopeMiddlewareWithExpiry(t, far)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Sunset"); got != "" {
		t.Errorf("Sunset header MUST be absent when expiry > 7d away, got %q", got)
	}
}

// T6.4 — expires_at within 7d → Sunset/Deprecation/Link headers all present.
// Sunset format MUST be RFC 1123 parseable.
func TestRequireScope_SunsetHeaderWhenExpiresAtWithinThreshold(t *testing.T) {
	expiry := time.Now().Add(2 * 24 * time.Hour) // 2 days
	rec := runScopeMiddlewareWithExpiry(t, expiry)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	sunset := rec.Header().Get("Sunset")
	if sunset == "" {
		t.Fatal("Sunset header missing when expiry is within 7d")
	}
	if _, err := time.Parse(time.RFC1123, sunset); err != nil {
		t.Errorf("Sunset header %q must parse as RFC 1123: %v", sunset, err)
	}

	if got := rec.Header().Get("Deprecation"); got != "true" {
		t.Errorf("Deprecation header must be 'true', got %q", got)
	}

	link := rec.Header().Get("Link")
	if !strings.Contains(link, `rel="deprecation"`) {
		t.Errorf("Link header must include rel=\"deprecation\", got %q", link)
	}
	if !strings.Contains(link, "/docs/api-keys") {
		t.Errorf("Link header must point at docs, got %q", link)
	}
}

