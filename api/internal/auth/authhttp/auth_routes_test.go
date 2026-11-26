// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package authhttp_test

import (
	"context"
	"encoding/json"
	"github.com/getloopscale/loop-runtime/api/internal/auth/authhttp"
	"net/http"
	"net/http/httptest"
	"testing"

	mwauth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	observability "github.com/getloopscale/loop-runtime/api/internal/middleware/observability"
)

func TestWhoamiRoute(t *testing.T) {
	t.Run("returns 200 with user info when authenticated", func(t *testing.T) {
		handler := authhttp.NewAuthHandler(nil)

		uc := &mwauth.UserContext{
			UserID:   "github-lucas",
			Email:    "lucas@acme.dev",
			TenantID: "acme",
			Role:     "owner",
		}
		ctx := context.WithValue(context.Background(), mwauth.UserContextKeyPublic(), uc)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil).WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.Whoami(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var body map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["user_id"] != "github-lucas" {
			t.Fatalf("expected user_id=github-lucas, got %q", body["user_id"])
		}
		if body["email"] != "lucas@acme.dev" {
			t.Fatalf("expected email=lucas@acme.dev, got %q", body["email"])
		}
		if body["team"] != "acme" {
			t.Fatalf("expected team=acme, got %q", body["team"])
		}
		if body["role"] != "owner" {
			t.Fatalf("expected role=owner, got %q", body["role"])
		}
	})

	t.Run("returns 401 when no UserContext (middleware not applied)", func(t *testing.T) {
		handler := authhttp.NewAuthHandler(nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil)
		rec := httptest.NewRecorder()

		handler.Whoami(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("returns noreply email when GitHub email is empty", func(t *testing.T) {
		handler := authhttp.NewAuthHandler(nil)

		uc := &mwauth.UserContext{
			UserID:   "github-getloopscale",
			Email:    "", // GitHub /user returns null for private emails
			TenantID: "acme",
			Role:     "owner",
		}
		ctx := context.WithValue(context.Background(), mwauth.UserContextKeyPublic(), uc)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil).WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.Whoami(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var body map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["email"] != "getloopscale@users.noreply.github.com" {
			t.Fatalf("expected noreply email, got %q", body["email"])
		}
	})

	t.Run("response has Correlation-ID header when present in request", func(t *testing.T) {
		handler := authhttp.NewAuthHandler(nil)

		uc := &mwauth.UserContext{
			UserID: "github-lucas", Email: "lucas@acme.dev", TenantID: "acme", Role: "owner",
		}
		ctx := context.WithValue(context.Background(), mwauth.UserContextKeyPublic(), uc)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil).WithContext(ctx)
		req.Header.Set("Correlation-ID", "test-corr-123")
		rec := httptest.NewRecorder()

		// Wrap with correlation middleware
		mw := observability.CorrelationID(http.HandlerFunc(handler.Whoami))
		mw.ServeHTTP(rec, req)

		corrID := rec.Header().Get("Correlation-ID")
		if corrID != "test-corr-123" {
			t.Fatalf("expected Correlation-ID=test-corr-123, got %q", corrID)
		}
	})
}
