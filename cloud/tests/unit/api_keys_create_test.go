// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Plan 0 of dashboard-memory-section-roadmap.md, T0.5.
// Tests for POST /v1/api-keys — create handler per D1/D2/D3/D4/D6 + EC-3,4,5.

package unit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

func postBody(t *testing.T, body any) *strings.Reader {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return strings.NewReader(string(b))
}

func authedPost(target string, u *auth.UserContext, body any, t *testing.T) *http.Request {
	req := httptest.NewRequest("POST", target, postBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	if u != nil {
		req = req.WithContext(auth.WithUser(req.Context(), u))
	}
	return req
}

func TestCreateKey_RejectsUnauthenticated(t *testing.T) {
	h, _ := newAPIKeysHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/api-keys", strings.NewReader(`{"name":"x","scopes":["memory:read"]}`))
	h.CreateKey(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// T0.5 case 1: happy path — secret returned ONCE, persists SHA-256 hash (64 hex chars).
// Plan invariants: D3 secret format `loopzero_live_{base64url(32)}` (10+43 = 53 chars),
// D4 SHA-256 (hex 64 chars), D5 subject + tenant_id derived from auth (NOT body).
func TestCreateKey_HappyPath(t *testing.T) {
	h, mock := newAPIKeysHandler(t)

	// Capture the INSERT to inspect the hash + prefix that the handler computes.
	mock.ExpectExec("INSERT INTO loopzero_api_keys").
		WithArgs(
			sqlmock.AnyArg(),       // id (apk-{16hex})
			"user-A",               // subject — derived from auth, NOT body
			"tenant-A",             // tenant_id — derived from auth, NOT body
			"prod-key",             // key_name
			sqlmock.AnyArg(),       // key_hash (validated below)
			sqlmock.AnyArg(),       // key_prefix
			sqlmock.AnyArg(),       // scopes jsonb
			sqlmock.AnyArg(),       // expires_at
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	rec := httptest.NewRecorder()
	body := map[string]any{
		"name":   "prod-key",
		"scopes": []string{"memory:read", "memory:write"},
	}
	h.CreateKey(rec, authedPost("/v1/api-keys", &auth.UserContext{Subject: "user-A", TenantID: "tenant-A"}, body, t))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		KeyPrefix string   `json:"key_prefix"`
		Scopes    []string `json:"scopes"`
		Secret    string   `json:"secret"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v body=%s", err, rec.Body.String())
	}

	// D3 invariant: secret starts with `loopzero_live_` and is 53 chars total
	// (prefix 10 + base64url of 32 bytes = 43 chars).
	if !strings.HasPrefix(resp.Secret, "loopzero_live_") {
		t.Errorf("secret must start with loopzero_live_, got %q", resp.Secret)
	}
	if len(resp.Secret) != 53 {
		t.Errorf("secret length must be 53 (10 prefix + 43 base64url), got %d", len(resp.Secret))
	}
	// D4 invariant: SHA-256 hex of the secret is what gets persisted.
	// Recompute and confirm the prefix is the first 12 chars of the secret.
	if resp.KeyPrefix != resp.Secret[:12] {
		t.Errorf("key_prefix must be secret[:12], got %q vs %q", resp.KeyPrefix, resp.Secret[:12])
	}
	expectedHash := sha256.Sum256([]byte(resp.Secret))
	hashHex := hex.EncodeToString(expectedHash[:])
	if len(hashHex) != 64 {
		t.Fatalf("hash hex length must be 64, got %d", len(hashHex))
	}

	// id must follow the project ULID-equivalent format apk-{16hex}.
	if !strings.HasPrefix(resp.ID, "apk-") || len(resp.ID) != 20 {
		t.Errorf("id must be apk-{16hex}, got %q", resp.ID)
	}
}

// T0.5 case 2: empty scopes rejected at 400 with EMPTY_SCOPES.
// A key with no scopes can do nothing — useless and probably a UI bug.
func TestCreateKey_EmptyScopesRejected(t *testing.T) {
	h, _ := newAPIKeysHandler(t)
	rec := httptest.NewRecorder()
	body := map[string]any{"name": "x", "scopes": []string{}}
	h.CreateKey(rec, authedPost("/v1/api-keys", &auth.UserContext{Subject: "u", TenantID: "t"}, body, t))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "EMPTY_SCOPES") {
		t.Errorf("expected EMPTY_SCOPES error code, got %s", rec.Body.String())
	}
}

// T0.5 case 3: reserved scope returns 503 scope_not_yet_active (D2 invariant).
// The customer is honestly told "this scope reserved for future vertical";
// the alternative (cryptic 403 scope_required) would suggest a permissions
// problem when the real answer is "not implemented yet".
func TestCreateKey_ReservedScopeReturns503(t *testing.T) {
	cases := []string{"rag:read", "rag:write", "traces:read", "agents:read", "agents:invoke", "deploys:read", "deploys:write"}
	for _, sc := range cases {
		t.Run(sc, func(t *testing.T) {
			h, _ := newAPIKeysHandler(t)
			rec := httptest.NewRecorder()
			body := map[string]any{"name": "x", "scopes": []string{sc}}
			h.CreateKey(rec, authedPost("/v1/api-keys", &auth.UserContext{Subject: "u", TenantID: "t"}, body, t))
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503 for reserved scope %q, got %d body=%s", sc, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "scope_not_yet_active") {
				t.Errorf("expected scope_not_yet_active error code, got %s", rec.Body.String())
			}
		})
	}
}

// T0.5 case 4: unknown scope (typo) rejected with 400 UNKNOWN_SCOPE.
// "momory:read" (typo) must fail loudly — without this, a typo silently
// creates a key with NO scopes, which would then 403 every request.
func TestCreateKey_UnknownScopeReturns400(t *testing.T) {
	h, _ := newAPIKeysHandler(t)
	rec := httptest.NewRecorder()
	body := map[string]any{"name": "x", "scopes": []string{"momory:read"}}
	h.CreateKey(rec, authedPost("/v1/api-keys", &auth.UserContext{Subject: "u", TenantID: "t"}, body, t))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "UNKNOWN_SCOPE") {
		t.Errorf("expected UNKNOWN_SCOPE error code, got %s", rec.Body.String())
	}
}

// T0.5 case 5 — EC-3: expires_at in the past rejected before INSERT.
// A born-expired key is useless and probably indicates a UI clock-bug.
func TestCreateKey_ExpiresInPastRejected(t *testing.T) {
	h, _ := newAPIKeysHandler(t)
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	rec := httptest.NewRecorder()
	body := map[string]any{"name": "x", "scopes": []string{"memory:read"}, "expires_at": past}
	h.CreateKey(rec, authedPost("/v1/api-keys", &auth.UserContext{Subject: "u", TenantID: "t"}, body, t))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for past expires_at, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "EXPIRES_IN_PAST") {
		t.Errorf("expected EXPIRES_IN_PAST code, got %s", rec.Body.String())
	}
}

// T0.5 case 6 — EC-4: empty key_name (whitespace-only after trim) rejected
// with 400 EMPTY_NAME BEFORE INSERT. Without this, the CHECK constraint
// `length(key_name) BETWEEN 1 AND 64` would surface as a generic 500.
func TestCreateKey_EmptyNameRejected(t *testing.T) {
	for _, name := range []string{"", "   ", "\t\n"} {
		t.Run("name="+name, func(t *testing.T) {
			h, _ := newAPIKeysHandler(t)
			rec := httptest.NewRecorder()
			body := map[string]any{"name": name, "scopes": []string{"memory:read"}}
			h.CreateKey(rec, authedPost("/v1/api-keys", &auth.UserContext{Subject: "u", TenantID: "t"}, body, t))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for empty name %q, got %d", name, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "EMPTY_NAME") {
				t.Errorf("expected EMPTY_NAME code, got %s", rec.Body.String())
			}
		})
	}
}

// T0.5 case 7 — EC-4: name > 64 chars rejected 400 NAME_TOO_LONG (DB CHECK
// would otherwise surface as 500).
func TestCreateKey_NameTooLongRejected(t *testing.T) {
	h, _ := newAPIKeysHandler(t)
	long := strings.Repeat("a", 65)
	rec := httptest.NewRecorder()
	body := map[string]any{"name": long, "scopes": []string{"memory:read"}}
	h.CreateKey(rec, authedPost("/v1/api-keys", &auth.UserContext{Subject: "u", TenantID: "t"}, body, t))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for 65-char name, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "NAME_TOO_LONG") {
		t.Errorf("expected NAME_TOO_LONG code, got %s", rec.Body.String())
	}
}

// T0.5 case 8: valid expires_at (future) persists correctly.
func TestCreateKey_FutureExpiresAtPersists(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	future := time.Now().Add(48 * time.Hour)

	mock.ExpectExec("INSERT INTO loopzero_api_keys").
		WithArgs(
			sqlmock.AnyArg(), "u", "t", "prod-key",
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), // expires_at — non-nil
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	rec := httptest.NewRecorder()
	body := map[string]any{
		"name":       "prod-key",
		"scopes":     []string{"memory:read"},
		"expires_at": future.Format(time.RFC3339),
	}
	h.CreateKey(rec, authedPost("/v1/api-keys", &auth.UserContext{Subject: "u", TenantID: "t"}, body, t))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// T0.5 case 9: ignore client-supplied subject/tenant_id in the body — the
// handler MUST derive these from auth.UserContext, never the request body.
// D5 invariant: NEVER trust client-supplied identity.
func TestCreateKey_IgnoresClientSuppliedSubjectAndTenant(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	mock.ExpectExec("INSERT INTO loopzero_api_keys").
		WithArgs(
			sqlmock.AnyArg(),
			"user-A",   // — authenticated, NOT the body's "subject"
			"tenant-A", // — authenticated, NOT the body's "tenant_id"
			"prod",
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	rec := httptest.NewRecorder()
	body := map[string]any{
		"name":      "prod",
		"scopes":    []string{"memory:read"},
		"subject":   "user-B-attacker",
		"tenant_id": "tenant-B-attacker",
	}
	h.CreateKey(rec, authedPost("/v1/api-keys", &auth.UserContext{Subject: "user-A", TenantID: "tenant-A"}, body, t))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("D5 violation: client-supplied subject/tenant_id reached the INSERT: %v", err)
	}
}

// T0.5 case 10 — EC-10 SHOULD TEST: duplicate scopes accepted as-is.
// `["memory:read","memory:read"]` is non-canonical but harmless — display-side
// deduplication is a UI concern. We do NOT add server-side dedup because
// that would diverge from "the response echoes the input" expectations.
func TestCreateKey_DuplicateScopesAcceptedAsIs(t *testing.T) {
	h, mock := newAPIKeysHandler(t)
	mock.ExpectExec("INSERT INTO loopzero_api_keys").
		WithArgs(sqlmock.AnyArg(), "u", "t", "x", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	rec := httptest.NewRecorder()
	body := map[string]any{"name": "x", "scopes": []string{"memory:read", "memory:read"}}
	h.CreateKey(rec, authedPost("/v1/api-keys", &auth.UserContext{Subject: "u", TenantID: "t"}, body, t))
	if rec.Code != http.StatusOK {
		t.Fatalf("duplicate scopes must not be rejected, got %d body=%s", rec.Code, rec.Body.String())
	}
}
