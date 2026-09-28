// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Auth cache behavior tests ---

func TestAuth_CacheHit_DoesNotCallValidatorAgain(t *testing.T) {
	// Arrange
	callCount := 0
	validator := &countingValidator{
		result:    &TokenValidationResult{UserID: "u-1", Email: "u@test.com"},
		callCount: &callCount,
	}
	store := &stubMembershipStore{
		memberships: []TenantMembership{{TenantID: "tn-1", Role: "owner"}},
	}

	authn := NewAuthenticator(Config{})
	handler := authn.MiddlewareWithTTL(validator, store, 5*time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Act: two requests with same token
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer same-token-123")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	// Assert: validator should only be called once (remaining served from cache)
	if callCount != 1 {
		t.Errorf("expected 1 validator call (cache hit for repeats), got %d", callCount)
	}
}

func TestAuth_MembershipStoreFails_Returns500(t *testing.T) {
	// Arrange
	validator := &stubValidator{
		result: &TokenValidationResult{UserID: "u-1", Email: "u@test.com"},
	}
	store := &stubMembershipStore{err: errors.New("db connection refused")}

	authn := NewAuthenticator(Config{})
	handler := authn.MiddlewareWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	// Act
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestAuth_MultipleTenants_WrongTenant_Returns403(t *testing.T) {
	// Arrange
	validator := &stubValidator{
		result: &TokenValidationResult{UserID: "u-multi", Email: "multi@test.com"},
	}
	store := &stubMembershipStore{
		memberships: []TenantMembership{
			{TenantID: "tn-a", Role: "owner"},
			{TenantID: "tn-b", Role: "member"},
		},
	}
	authn := NewAuthenticator(Config{})
	handler := authn.MiddlewareWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	// Act: request with tenant that user is NOT a member of
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("X-LoopZero-Tenant", "tn-nonexistent")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "TENANT_NOT_FOUND" {
		t.Errorf("expected TENANT_NOT_FOUND code, got %q", body["code"])
	}
}

func TestAuth_BearerPrefix_CaseSensitive(t *testing.T) {
	// "bearer" (lowercase) should be rejected
	validator := &stubValidator{result: &TokenValidationResult{}}
	store := &stubMembershipStore{}
	authn := NewAuthenticator(Config{})
	handler := authn.MiddlewareWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "bearer lowercase-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for lowercase bearer, got %d", rec.Code)
	}
}

func TestAuth_ServiceAuth_MissingIdentity_FallsToBearer(t *testing.T) {
	// Service auth with valid secret but missing identity headers:
	// valid secret passes, but empty userID/tenantID skips context injection,
	// falls through to Bearer token extraction which returns 401 (no Authorization header).

	store := &stubMembershipStore{}
	validator := &stubValidator{}
	authn := NewAuthenticator(Config{ServiceSecret: "valid-secret"})
	handler := authn.MiddlewareWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Service-Auth", "valid-secret")
	// No X-LoopZero-User-ID, no X-LoopZero-Tenant, no Bearer token
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (falls to Bearer path), got %d", rec.Code)
	}
}

func TestAuth_ServiceAuth_MembershipNotFound_Returns403(t *testing.T) {
	store := &stubMembershipStore{
		memberships: []TenantMembership{{TenantID: "tn-other", Role: "owner"}},
	}
	validator := &stubValidator{}
	authn := NewAuthenticator(Config{ServiceSecret: "valid-secret"})
	handler := authn.MiddlewareWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Service-Auth", "valid-secret")
	req.Header.Set("X-LoopZero-User-ID", "svc-user")
	req.Header.Set("X-LoopZero-Tenant", "tn-unknown") // not in memberships
	req.Header.Set("X-LoopZero-Email", "svc@test.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestAuth_ServiceAuth_PreviousSecret_DualKeyRotation(t *testing.T) {
	store := &stubMembershipStore{
		memberships: []TenantMembership{{TenantID: "tn-1", Role: "admin"}},
	}

	var captured *UserContext
	authn := NewAuthenticator(Config{ServiceSecret: "new-secret", ServiceSecretPrevious: "old-secret"})
	handler := authn.MiddlewareWithTTL(&stubValidator{}, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = GetUserContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Service-Auth", "old-secret") // previous secret
	req.Header.Set("X-LoopZero-User-ID", "svc-user")
	req.Header.Set("X-LoopZero-Tenant", "tn-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with previous secret during rotation, got %d", rec.Code)
	}
	if captured == nil || captured.UserID != "svc-user" {
		t.Error("expected user context to be set via previous service secret")
	}
}

// --- Roles extended tests ---

func TestRequireRole_MemberRequired_MemberPasses(t *testing.T) {
	handler := RequireRole("member")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	ctx := context.WithValue(req.Context(), userContextKey, &UserContext{
		UserID: "u-1", TenantID: "tn-1", Role: "member",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code != http.StatusOK {
		t.Errorf("member should pass member check, got %d", rec.Code)
	}
}

func TestRequireRole_OwnerRequired_AdminFails(t *testing.T) {
	handler := RequireRole("owner")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	ctx := context.WithValue(req.Context(), userContextKey, &UserContext{
		UserID: "u-1", TenantID: "tn-1", Role: "admin",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code != http.StatusForbidden {
		t.Errorf("admin should fail owner check, got %d", rec.Code)
	}
}

func TestRequireRole_ForbiddenResponse_HasJSON(t *testing.T) {
	handler := RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	ctx := context.WithValue(req.Context(), userContextKey, &UserContext{
		UserID: "u-1", TenantID: "tn-1", Role: "member",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body["code"] != "INSUFFICIENT_ROLE" {
		t.Errorf("expected INSUFFICIENT_ROLE code, got %q", body["code"])
	}
}

// --- GetUserContext tests ---

func TestGetUserContext_NoContext_ReturnsNil(t *testing.T) {
	ctx := context.Background()
	uc := GetUserContext(ctx)
	if uc != nil {
		t.Error("expected nil UserContext for empty context")
	}
}

func TestGetUserContext_WithContext_ReturnsValue(t *testing.T) {
	expected := &UserContext{UserID: "u-1", TenantID: "tn-1", Email: "test@test.com", Role: "owner"}
	ctx := context.WithValue(context.Background(), userContextKey, expected)
	uc := GetUserContext(ctx)
	if uc == nil {
		t.Fatal("expected non-nil UserContext")
	}
	if uc.UserID != "u-1" || uc.TenantID != "tn-1" || uc.Role != "owner" {
		t.Errorf("unexpected UserContext: %+v", uc)
	}
}

// --- Test doubles ---

type countingValidator struct {
	result    *TokenValidationResult
	callCount *int
}

func (v *countingValidator) ValidateToken(_ context.Context, _ string) (*TokenValidationResult, error) {
	*v.callCount++
	return v.result, nil
}
