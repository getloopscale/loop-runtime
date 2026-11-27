// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
)

type fakeTokenValidator struct {
	result *auth.TokenValidationResult
	err    error
	calls  int
}

func (f *fakeTokenValidator) ValidateToken(ctx context.Context, token string) (*auth.TokenValidationResult, error) {
	f.calls++
	return f.result, f.err
}

type fakeMembershipStore struct {
	memberships []auth.TenantMembership
}

func (f *fakeMembershipStore) GetMemberships(ctx context.Context, userID string) ([]auth.TenantMembership, error) {
	return f.memberships, nil
}

func TestAuthMiddleware(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uc := auth.GetUserContext(r.Context())
		if uc == nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"user_id":   uc.UserID,
			"email":     uc.Email,
			"tenant_id": uc.TenantID,
			"role":      uc.Role,
		})
	})

	t.Run("no Authorization header → 401", func(t *testing.T) {
		validator := &fakeTokenValidator{}
		store := &fakeMembershipStore{}
		mw := auth.Auth(auth.NewAuthenticator(auth.Config{}), validator, store)
		handler := mw(okHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		var body map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["code"] != "UNAUTHORIZED" {
			t.Fatalf("expected code=UNAUTHORIZED, got %q", body["code"])
		}
	})

	t.Run("invalid token → 401", func(t *testing.T) {
		validator := &fakeTokenValidator{result: nil, err: auth.ErrInvalidToken}
		store := &fakeMembershipStore{}
		mw := auth.Auth(auth.NewAuthenticator(auth.Config{}), validator, store)
		handler := mw(okHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer REDACTED_SECRET_063")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("valid token + 1 tenant → auto-resolve + UserContext", func(t *testing.T) {
		validator := &fakeTokenValidator{
			result: &auth.TokenValidationResult{
				UserID: "github-lucas",
				Email:  "lucas@acme.dev",
			},
		}
		store := &fakeMembershipStore{
			memberships: []auth.TenantMembership{
				{TenantID: "acme", Role: "owner"},
			},
		}
		mw := auth.Auth(auth.NewAuthenticator(auth.Config{}), validator, store)
		handler := mw(okHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer ghp_valid")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var body map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["user_id"] != "github-lucas" {
			t.Fatalf("expected user_id=github-lucas, got %q", body["user_id"])
		}
		if body["tenant_id"] != "acme" {
			t.Fatalf("expected tenant_id=acme, got %q", body["tenant_id"])
		}
		if body["role"] != "owner" {
			t.Fatalf("expected role=owner, got %q", body["role"])
		}
	})

	t.Run("valid token + N tenants + X-Loop-Runtime-Tenant → resolve", func(t *testing.T) {
		validator := &fakeTokenValidator{
			result: &auth.TokenValidationResult{
				UserID: "github-lucas", Email: "lucas@acme.dev",
			},
		}
		store := &fakeMembershipStore{
			memberships: []auth.TenantMembership{
				{TenantID: "acme", Role: "owner"},
				{TenantID: "globex", Role: "member"},
			},
		}
		mw := auth.Auth(auth.NewAuthenticator(auth.Config{}), validator, store)
		handler := mw(okHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer ghp_valid")
		req.Header.Set("X-Loop-Runtime-Tenant", "globex")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var body map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["tenant_id"] != "globex" {
			t.Fatalf("expected tenant_id=globex, got %q", body["tenant_id"])
		}
	})

	t.Run("valid token + N tenants + no X-Loop-Runtime-Tenant → 409 TENANT_REQUIRED", func(t *testing.T) {
		validator := &fakeTokenValidator{
			result: &auth.TokenValidationResult{
				UserID: "github-lucas", Email: "lucas@acme.dev",
			},
		}
		store := &fakeMembershipStore{
			memberships: []auth.TenantMembership{
				{TenantID: "acme", Role: "owner"},
				{TenantID: "globex", Role: "member"},
			},
		}
		mw := auth.Auth(auth.NewAuthenticator(auth.Config{}), validator, store)
		handler := mw(okHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer ghp_valid")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", rec.Code)
		}

		var body map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["code"] != "TENANT_REQUIRED" {
			t.Fatalf("expected code=TENANT_REQUIRED, got %q", body["code"])
		}
	})

	t.Run("valid token + 0 tenants → 403 NO_TENANT", func(t *testing.T) {
		validator := &fakeTokenValidator{
			result: &auth.TokenValidationResult{
				UserID: "github-lucas", Email: "lucas@acme.dev",
			},
		}
		store := &fakeMembershipStore{memberships: nil}
		mw := auth.Auth(auth.NewAuthenticator(auth.Config{}), validator, store)
		handler := mw(okHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer ghp_valid")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}

		var body map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["code"] != "NO_TENANT" {
			t.Fatalf("expected code=NO_TENANT, got %q", body["code"])
		}
	})

	t.Run("X-Loop-Runtime-Tenant with non-member tenant → 403", func(t *testing.T) {
		validator := &fakeTokenValidator{
			result: &auth.TokenValidationResult{
				UserID: "github-lucas", Email: "lucas@acme.dev",
			},
		}
		store := &fakeMembershipStore{
			memberships: []auth.TenantMembership{
				{TenantID: "acme", Role: "owner"},
				{TenantID: "globex", Role: "member"},
			},
		}
		mw := auth.Auth(auth.NewAuthenticator(auth.Config{}), validator, store)
		handler := mw(okHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer ghp_valid")
		req.Header.Set("X-Loop-Runtime-Tenant", "unknown-tenant")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})

	t.Run("cache hit: second call with same token does not call validator again", func(t *testing.T) {
		validator := &fakeTokenValidator{
			result: &auth.TokenValidationResult{
				UserID: "github-lucas", Email: "lucas@acme.dev",
			},
		}
		store := &fakeMembershipStore{
			memberships: []auth.TenantMembership{
				{TenantID: "acme", Role: "owner"},
			},
		}
		mw := auth.Auth(auth.NewAuthenticator(auth.Config{}), validator, store)
		handler := mw(okHandler)

		// First call
		req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
		req1.Header.Set("Authorization", "Bearer ghp_cached")
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)

		// Second call — same token
		req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
		req2.Header.Set("Authorization", "Bearer ghp_cached")
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)

		if validator.calls != 1 {
			t.Fatalf("expected 1 validator call (cached), got %d", validator.calls)
		}
	})

	t.Run("cache expired: validator called again after TTL elapses", func(t *testing.T) {
		validator := &fakeTokenValidator{
			result: &auth.TokenValidationResult{
				UserID: "github-lucas", Email: "lucas@acme.dev",
			},
		}
		store := &fakeMembershipStore{
			memberships: []auth.TenantMembership{
				{TenantID: "acme", Role: "owner"},
			},
		}
		// Use 1ms TTL so the cache expires almost immediately
		mw := auth.NewAuthenticator(auth.Config{}).MiddlewareWithTTL(validator, store, 1*time.Millisecond)
		handler := mw(okHandler)

		// First call — populates cache
		req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
		req1.Header.Set("Authorization", "Bearer ghp_expiry")
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)

		if validator.calls != 1 {
			t.Fatalf("expected 1 call after first request, got %d", validator.calls)
		}

		// intentional: testing real-time behavior — wait for 1ms TTL cache to expire
		time.Sleep(5 * time.Millisecond)

		// Second call — cache expired, should call validator again
		req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
		req2.Header.Set("Authorization", "Bearer ghp_expiry")
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)

		if validator.calls != 2 {
			t.Fatalf("expected 2 calls after cache expiry, got %d", validator.calls)
		}

		if rec2.Code != http.StatusOK {
			t.Fatalf("expected 200 after re-validation, got %d", rec2.Code)
		}
	})
}
