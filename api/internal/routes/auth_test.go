// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/loopzero/loopzero/api/internal/middleware"
)

func TestAuth_Whoami_HappyPath(t *testing.T) {
	h := NewAuthHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil)
	req = withAuth(req, defaultUserContext())
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
}

func TestAuth_Whoami_Unauthenticated(t *testing.T) {
	h := NewAuthHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil)
	w := httptest.NewRecorder()

	h.Whoami(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAuth_Whoami_PrivateEmail_DeriveNoreply(t *testing.T) {
	h := NewAuthHandler()

	uc := &middleware.UserContext{
		UserID:   "github-jdoe",
		Email:    "", // private email
		TenantID: "tn-test-123",
		Role:     "member",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil)
	req = withAuth(req, uc)
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
