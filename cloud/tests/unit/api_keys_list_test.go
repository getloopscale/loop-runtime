// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Plan 0 of dashboard-memory-section-roadmap.md, T0.3.
// Tests for GET /v1/api-keys/list — list handler that filters by
// (subject, tenant_id), excludes soft-deleted rows, and applies the
// EC-1 LIMIT 100 cap.

package unit

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	"github.com/loopzero/loopzero/cloud/internal/routes"
)

// authedRequest returns a GET request with the given UserContext attached via
// the canonical auth.WithUser helper. Centralises the test boilerplate so the
// authentication shape is uniform across all api-keys tests.
func authedRequest(method, target string, u *auth.UserContext) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	if u != nil {
		req = req.WithContext(auth.WithUser(req.Context(), u))
	}
	return req
}

func newAPIKeysHandler(t *testing.T) (*routes.APIKeysHandler, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	logger := slog.New(slog.NewTextHandler(httptest.NewRecorder().Body, nil))
	return routes.NewAPIKeysHandler(db, logger), mock
}

// T0.3 — unauthenticated request rejected with AUTH_REQUIRED (parity with
// billing/me handlers). The contract here is `auth.GetUser == nil → 401`.
func TestListKeys_RejectsUnauthenticated(t *testing.T) {
	h, _ := newAPIKeysHandler(t)
	rec := httptest.NewRecorder()
	h.ListKeys(rec, httptest.NewRequest("GET", "/v1/api-keys/list", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// T0.3 case 1: empty result — user has zero active keys.
// Behavior: 200 + {"keys": []} (never null — JSON consumers expect array).
func TestListKeys_EmptyResult(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	mock.ExpectQuery("SELECT id, key_name, key_prefix, scopes, created_at, last_used_at, expires_at").
		WithArgs("user-A", "tenant-A").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "key_name", "key_prefix", "scopes", "created_at", "last_used_at", "expires_at",
		}))

	rec := httptest.NewRecorder()
	h.ListKeys(rec, authedRequest("GET", "/v1/api-keys/list", &auth.UserContext{Subject: "user-A", TenantID: "tenant-A"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v body=%s", err, rec.Body.String())
	}
	if resp.Keys == nil {
		t.Fatal("keys must be empty array, not null")
	}
	if len(resp.Keys) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(resp.Keys))
	}
}

// T0.3 case 2: 3 active keys filtered by (subject, tenant_id).
// Plan invariant D5: every list query MUST filter by (subject, tenant_id) —
// failure mode would leak other users' keys.
func TestListKeys_ThreeActiveKeysFiltered(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "key_name", "key_prefix", "scopes", "created_at", "last_used_at", "expires_at",
	}).
		AddRow("apk-0000000000000001", "prod", "loopzero_live_Ab", []byte(`["memory:read"]`), now, nil, nil).
		AddRow("apk-0000000000000002", "staging", "loopzero_live_Cd", []byte(`["memory:read","memory:write"]`), now, &now, nil).
		AddRow("apk-0000000000000003", "ci", "loopzero_live_Ef", []byte(`["memory:admin"]`), now, nil, nil)

	mock.ExpectQuery("SELECT id, key_name, key_prefix, scopes").
		WithArgs("user-A", "tenant-A").
		WillReturnRows(rows)

	rec := httptest.NewRecorder()
	h.ListKeys(rec, authedRequest("GET", "/v1/api-keys/list", &auth.UserContext{Subject: "user-A", TenantID: "tenant-A"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Keys []map[string]any `json:"keys"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(resp.Keys))
	}
	// Spot-check the second key's scopes serialise as a slice of strings (NOT a
	// raw jsonb string), so the dashboard can render `Badge` per scope.
	scopes, ok := resp.Keys[1]["scopes"].([]any)
	if !ok {
		t.Fatalf("scopes must be JSON array on the wire, got %T", resp.Keys[1]["scopes"])
	}
	if len(scopes) != 2 {
		t.Fatalf("expected 2 scopes on second row, got %d", len(scopes))
	}
}

// T0.3 case 3: EC-1 absorption — LIMIT 100 cap. The query MUST contain
// LIMIT 100 so that a user accumulating many keys does not get a runaway
// response (even though the unique-name CHECK does not exist, nothing prevents
// programmatic creation of thousands). We assert via sqlmock query-string
// matching that the SQL contains "LIMIT 100".
func TestListKeys_QueryContainsLIMIT100(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	// QueryMatcherEqual is the default — sqlmock asserts the literal SQL.
	// We use the regex matcher implicitly: ExpectQuery treats the arg as a
	// regex. Embed `LIMIT 100` so a missing-cap handler fails this match.
	mock.ExpectQuery("LIMIT 100").
		WithArgs("user-A", "tenant-A").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "key_name", "key_prefix", "scopes", "created_at", "last_used_at", "expires_at",
		}))

	rec := httptest.NewRecorder()
	h.ListKeys(rec, authedRequest("GET", "/v1/api-keys/list", &auth.UserContext{Subject: "user-A", TenantID: "tenant-A"}))

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("LIMIT 100 cap missing from list query (EC-1 absorption regression): %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// T0.3 case 4: cross-tenant invisible — the WHERE tenant_id = $2 predicate
// is what enforces M1 multi-tenancy at the query level. We assert via
// sqlmock that the args match the authenticated user's tenant, not any
// client-supplied value.
func TestListKeys_QueryFiltersByAuthenticatedTenant(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	mock.ExpectQuery("SELECT id, key_name").
		WithArgs("user-A", "tenant-A"). // NOT a client-supplied value — derived from auth.UserContext
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "key_name", "key_prefix", "scopes", "created_at", "last_used_at", "expires_at",
		}))

	rec := httptest.NewRecorder()
	// Attacker would try to override tenant via header / query — we DON'T
	// give the handler a way to read those. Build the request with a
	// tampered query string and verify the handler ignores it.
	req := authedRequest("GET", "/v1/api-keys/list?tenant_id=tenant-B&subject=user-B", &auth.UserContext{Subject: "user-A", TenantID: "tenant-A"})
	h.ListKeys(rec, req)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("list query did NOT filter by authenticated (subject, tenant_id): %v", err)
	}
}
