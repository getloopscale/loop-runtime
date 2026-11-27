// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Split out of the flat `middleware` package's middleware_test.go (B-059): a
// test file lives in the package of its subject, and that file tested six
// middlewares that now live in five packages.

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Auth middleware tests ---

type stubValidator struct {
	result *TokenValidationResult
	err    error
}

func (v *stubValidator) ValidateToken(_ context.Context, _ string) (*TokenValidationResult, error) {
	return v.result, v.err
}

type stubMembershipStore struct {
	memberships []TenantMembership
	err         error
}

func (s *stubMembershipStore) GetMemberships(_ context.Context, _ string) ([]TenantMembership, error) {
	return s.memberships, s.err
}

func TestAuth_ValidToken_SetsUserContext(t *testing.T) {
	// Arrange
	validator := &stubValidator{
		result: &TokenValidationResult{UserID: "user-1", Email: "user@test.com"},
	}
	store := &stubMembershipStore{
		memberships: []TenantMembership{{TenantID: "tn-acme", Role: "owner"}},
	}

	var captured *UserContext
	authn := NewAuthenticator(Config{})
	handler := authn.MiddlewareWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = GetUserContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	// Act
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if captured == nil {
		t.Fatal("UserContext not set")
	}
	if captured.UserID != "user-1" {
		t.Errorf("UserID = %q, want user-1", captured.UserID)
	}
	if captured.TenantID != "tn-acme" {
		t.Errorf("TenantID = %q, want tn-acme", captured.TenantID)
	}
	if captured.Role != "owner" {
		t.Errorf("Role = %q, want owner", captured.Role)
	}
}

func TestAuth_MissingHeader_Returns401(t *testing.T) {
	// Arrange
	validator := &stubValidator{result: &TokenValidationResult{}}
	store := &stubMembershipStore{}
	authn := NewAuthenticator(Config{})
	handler := authn.MiddlewareWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	// Act
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuth_ExpiredToken_Returns401(t *testing.T) {
	// Arrange
	validator := &stubValidator{err: ErrInvalidToken}
	store := &stubMembershipStore{}
	authn := NewAuthenticator(Config{})
	handler := authn.MiddlewareWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	// Act
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer expired-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuth_NoMemberships_Returns403(t *testing.T) {
	// Arrange
	validator := &stubValidator{
		result: &TokenValidationResult{UserID: "user-orphan", Email: "orphan@test.com"},
	}
	store := &stubMembershipStore{memberships: nil}
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
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestAuth_MultipleTenants_NeedsHeader(t *testing.T) {
	// Arrange
	validator := &stubValidator{
		result: &TokenValidationResult{UserID: "user-multi", Email: "multi@test.com"},
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

	// Act: no X-Loop-Runtime-Tenant header
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}
}

func TestAuth_MultipleTenants_WithHeader_Resolves(t *testing.T) {
	// Arrange
	validator := &stubValidator{
		result: &TokenValidationResult{UserID: "user-multi", Email: "multi@test.com"},
	}
	store := &stubMembershipStore{
		memberships: []TenantMembership{
			{TenantID: "tn-a", Role: "owner"},
			{TenantID: "tn-b", Role: "member"},
		},
	}

	var captured *UserContext
	authn := NewAuthenticator(Config{})
	handler := authn.MiddlewareWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = GetUserContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	// Act
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("X-Loop-Runtime-Tenant", "tn-b")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if captured == nil || captured.TenantID != "tn-b" {
		t.Errorf("expected tenant tn-b, got %v", captured)
	}
	if captured.Role != "member" {
		t.Errorf("expected role member, got %q", captured.Role)
	}
}

func TestAuth_ServiceAuth_ValidSecret(t *testing.T) {
	// Arrange

	store := &stubMembershipStore{
		memberships: []TenantMembership{{TenantID: "tn-svc", Role: "admin"}},
	}
	validator := &stubValidator{} // should not be called

	var captured *UserContext
	authn := NewAuthenticator(Config{ServiceSecret: "test-service-secret"})
	handler := authn.MiddlewareWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = GetUserContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	// Act
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Service-Auth", "test-service-secret")
	req.Header.Set("X-Loop-Runtime-User-ID", "svc-user")
	req.Header.Set("X-Loop-Runtime-Tenant", "tn-svc")
	req.Header.Set("X-Loop-Runtime-Email", "svc@test.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if captured == nil || captured.UserID != "svc-user" {
		t.Errorf("expected svc-user context, got %v", captured)
	}
}

func TestAuth_ServiceAuth_InvalidSecret_Returns401(t *testing.T) {
	// Arrange

	store := &stubMembershipStore{}
	validator := &stubValidator{}
	authn := NewAuthenticator(Config{ServiceSecret: "correct-secret"})
	handler := authn.MiddlewareWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	// Act
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Service-Auth", "wrong-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuth_ValidatorError_Returns503(t *testing.T) {
	// Arrange: non-ErrInvalidToken error = service unavailable
	validator := &stubValidator{err: errors.New("connection refused")}
	store := &stubMembershipStore{}
	authn := NewAuthenticator(Config{})
	handler := authn.MiddlewareWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	// Act
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}
