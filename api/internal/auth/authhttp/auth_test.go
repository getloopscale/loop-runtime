// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package authhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/routestest"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
)

// fakeUsernameLookup implements UsernameLookup for tests (AUDIT-10).
type fakeUsernameLookup struct {
	byID map[string]string
	err  error
}

func (f *fakeUsernameLookup) GetUsernameByID(_ context.Context, userID string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.byID[userID], nil
}

func TestAuth_Whoami_HappyPath(t *testing.T) {
	h := NewAuthHandler(&fakeUsernameLookup{byID: map[string]string{"github-testuser": "testuser"}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Whoami(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["user_id"] != "github-testuser" {
		t.Fatalf("want user_id github-testuser, got %s", resp["user_id"])
	}
	if resp["email"] != "test@example.com" {
		t.Fatalf("want email test@example.com, got %s", resp["email"])
	}
	if resp["team"] != "tn-test-123" {
		t.Fatalf("want team tn-test-123, got %s", resp["team"])
	}
	if resp["role"] != "owner" {
		t.Fatalf("want role owner, got %s", resp["role"])
	}
	if resp["username"] != "testuser" {
		t.Fatalf("want username testuser, got %s", resp["username"])
	}
}

func TestAuth_Whoami_Unauthenticated(t *testing.T) {
	h := NewAuthHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil)
	w := httptest.NewRecorder()

	h.Whoami(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAuth_Whoami_PrivateEmail_DeriveNoreply(t *testing.T) {
	h := NewAuthHandler(nil)

	uc := &auth.UserContext{
		UserID:   "github-jdoe",
		Email:    "", // private email
		TenantID: "tn-test-123",
		Role:     "member",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil)
	req = routestest.WithAuth(req, uc)
	w := httptest.NewRecorder()

	h.Whoami(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["email"] != "jdoe@users.noreply.github.com" {
		t.Fatalf("want noreply email, got %s", resp["email"])
	}
}

// AUDIT-10: service accounts (OAuth2 client_credentials) have no email claim
// and no "github-" prefix. Previous handler returned email="" without fallback.
// Now username comes from users.username column (populated by migration 000037).
func TestAuth_Whoami_ServiceAccount_UsesUsername(t *testing.T) {
	h := NewAuthHandler(&fakeUsernameLookup{byID: map[string]string{"loopzero-e2e": "loopzero-e2e"}})

	uc := &auth.UserContext{
		UserID:   "loopzero-e2e", // no github- prefix
		Email:    "",             // no email for client_credentials
		TenantID: "e2e-tenant",
		Role:     "owner",
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil)
	req = routestest.WithAuth(req, uc)
	w := httptest.NewRecorder()

	h.Whoami(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["username"] != "loopzero-e2e" {
		t.Fatalf("want username loopzero-e2e, got %q", resp["username"])
	}
	if resp["email"] != "" {
		t.Fatalf("service account must not invent an email, got %q", resp["email"])
	}
}
