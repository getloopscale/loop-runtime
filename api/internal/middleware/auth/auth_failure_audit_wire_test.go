// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package auth

import (
	"context"
	"errors"
	"github.com/getloopscale/loop-runtime/api/internal/middleware/clientip"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// T1.3 wire test (Phase A.4 of MEET-2026-05-12).
//
// Verifies that each failure path in this package's Auth/Admin middlewares
// invokes the AuthFailureAuditor configured on the Authenticator. The auditor itself has separate tests
// covering rate limiting and metadata enrichment.

// captureAuditor records every emission for assertion.
type captureAuditor struct {
	mu      sync.Mutex
	events  []capturedEvent
	auditor *AuthFailureAuditor // the auditor to put in an Authenticator's Config
}

type capturedEvent struct {
	action AuthFailureAction
	reason string
	ip     string
}

func newCapturingDefault() *captureAuditor {
	c := &captureAuditor{}
	writer := func(_ context.Context, _, action, _, _, _, _ string, metadata map[string]any) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		reason, _ := metadata["reason"].(string)
		ip, _ := metadata["ip"].(string)
		c.events = append(c.events, capturedEvent{
			action: AuthFailureAction(action),
			reason: reason,
			ip:     ip,
		})
		return nil
	}
	c.auditor = NewAuthFailureAuditor(writer, slog.Default(), clientip.Resolver{})
	return c
}

// captured returns a snapshot. Caller must not modify.
func (c *captureAuditor) captured() []capturedEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]capturedEvent, len(c.events))
	copy(out, c.events)
	return out
}

// -----------------------------------------------------------------------------
// RequireAdminService
// -----------------------------------------------------------------------------

func TestAuthFailureAudit_Wire_RequireAdminService_MissingHeader(t *testing.T) {
	cap := newCapturingDefault()

	authn := NewAuthenticator(Config{Auditor: cap.auditor})
	handler := authn.RequireAdminService(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/x", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", w.Code)
	}
	events := cap.captured()
	if len(events) != 1 {
		t.Fatalf("want 1 audit event, got %d", len(events))
	}
	if events[0].action != AuthFailureServiceMismatch {
		t.Errorf("action = %q, want %q", events[0].action, AuthFailureServiceMismatch)
	}
	if events[0].reason == "" {
		t.Error("reason metadata missing")
	}
}

// TestRequireAdminService_RejectsLegacySecret asserts the qa-sprint2-cleanup-2026-06-24
// EC-1 narrow (closes loop-console#16): post-T1.0 the middleware accepts ONLY the
// canonical LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET (+ its _PREVIOUS rotation sibling) — NOT
// the legacy LOOP_RUNTIME_SERVICE_AUTH_SECRET that the cloud /engine/* proxy carries on every
// forwarded user-JWT request. Before T1.0 the middleware also matched `legacy`, so
// any tenant-owner JWT routed via the proxy passed the admin auth gate; the QA Red
// Team filed #16 against that bypass.
func TestRequireAdminService_RejectsLegacySecret(t *testing.T) {
	authn := NewAuthenticator(Config{AdminServiceSecret: "canonical-admin-secret", ServiceSecret: "legacy-proxy-secret"})
	handler := authn.RequireAdminService(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not be called when only the legacy proxy secret is provided")
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/x", nil)
	req.Header.Set("X-Service-Auth", "legacy-proxy-secret") // the secret the proxy adds
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 (middleware narrowed to canonical-only), got %d", w.Code)
	}
}

// TestRequireAdminService_AcceptsCanonical asserts the canonical secret still authorises
// admin requests post-T1.0 (the narrow MUST NOT break legitimate operator-side calls).
func TestRequireAdminService_AcceptsCanonical(t *testing.T) {
	called := false
	authn := NewAuthenticator(Config{AdminServiceSecret: "canonical-admin-secret", ServiceSecret: "legacy-proxy-secret"})
	handler := authn.RequireAdminService(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/x", nil)
	req.Header.Set("X-Service-Auth", "canonical-admin-secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 with canonical secret, got %d", w.Code)
	}
	if !called {
		t.Fatal("next handler must be invoked when canonical secret matches")
	}
}

// TestRequireAdminService_AcceptsCanonicalPrevious asserts the canonical _PREVIOUS
// rotation sibling still authorises (rotation use case preserved).
func TestRequireAdminService_AcceptsCanonicalPrevious(t *testing.T) {
	called := false
	authn := NewAuthenticator(Config{AdminServiceSecret: "new-canonical-admin-secret", AdminServiceSecretPrevious: "old-canonical-admin-secret"})
	handler := authn.RequireAdminService(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/x", nil)
	req.Header.Set("X-Service-Auth", "old-canonical-admin-secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 with canonical _PREVIOUS, got %d", w.Code)
	}
	if !called {
		t.Fatal("next handler must be invoked when canonical _PREVIOUS matches")
	}
}

// -----------------------------------------------------------------------------
// AuthWithTTL — Bearer path
// -----------------------------------------------------------------------------

type wireStubValidator struct {
	result *TokenValidationResult
	err    error
}

func (v *wireStubValidator) ValidateToken(_ context.Context, _ string) (*TokenValidationResult, error) {
	return v.result, v.err
}

type wireStubStore struct {
	memberships []TenantMembership
	err         error
}

func (s *wireStubStore) GetMemberships(_ context.Context, _ string) ([]TenantMembership, error) {
	return s.memberships, s.err
}

func TestAuthFailureAudit_Wire_Bearer_MissingHeader(t *testing.T) {
	cap := newCapturingDefault()

	authn := NewAuthenticator(Config{Auditor: cap.auditor})
	handler := authn.MiddlewareWithTTL(
		&wireStubValidator{result: &TokenValidationResult{UserID: "u-1"}},
		&wireStubStore{memberships: []TenantMembership{{TenantID: "tn-1", Role: "owner"}}},
		time.Minute,
	)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	// No Authorization header
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	events := cap.captured()
	if len(events) != 1 || events[0].action != AuthFailureTokenInvalid {
		t.Fatalf("want 1 AuthFailureTokenInvalid event, got %+v", events)
	}
}

func TestAuthFailureAudit_Wire_Bearer_InvalidToken(t *testing.T) {
	cap := newCapturingDefault()

	authn := NewAuthenticator(Config{Auditor: cap.auditor})
	handler := authn.MiddlewareWithTTL(
		&wireStubValidator{err: ErrInvalidToken},
		&wireStubStore{},
		time.Minute,
	)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	req.Header.Set("Authorization", "Bearer bad-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	events := cap.captured()
	if len(events) != 1 || events[0].action != AuthFailureTokenInvalid {
		t.Fatalf("want 1 AuthFailureTokenInvalid event, got %+v", events)
	}
}

func TestAuthFailureAudit_Wire_Bearer_TransientInfra_NoEmit(t *testing.T) {
	cap := newCapturingDefault()

	authn := NewAuthenticator(Config{Auditor: cap.auditor})
	handler := authn.MiddlewareWithTTL(
		&wireStubValidator{err: errors.New("JWKS unreachable")},
		&wireStubStore{},
		time.Minute,
	)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 (transient infra), got %d", w.Code)
	}
	events := cap.captured()
	if len(events) != 0 {
		t.Errorf("transient infra must NOT emit audit (alert fatigue): got %+v", events)
	}
}

func TestAuthFailureAudit_Wire_Bearer_ZeroMemberships(t *testing.T) {
	cap := newCapturingDefault()

	authn := NewAuthenticator(Config{Auditor: cap.auditor})
	handler := authn.MiddlewareWithTTL(
		&wireStubValidator{result: &TokenValidationResult{UserID: "u-orphan"}},
		&wireStubStore{memberships: nil}, // zero memberships
		time.Minute,
	)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	req.Header.Set("Authorization", "Bearer valid")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	events := cap.captured()
	if len(events) != 1 || events[0].action != AuthFailureTenantNotFound {
		t.Fatalf("want 1 AuthFailureTenantNotFound event, got %+v", events)
	}
}

func TestAuthFailureAudit_Wire_Bearer_NonMemberTenant(t *testing.T) {
	cap := newCapturingDefault()

	authn := NewAuthenticator(Config{Auditor: cap.auditor})
	handler := authn.MiddlewareWithTTL(
		&wireStubValidator{result: &TokenValidationResult{UserID: "u-multi"}},
		&wireStubStore{memberships: []TenantMembership{
			{TenantID: "tn-a", Role: "owner"},
			{TenantID: "tn-b", Role: "owner"},
		}},
		time.Minute,
	)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	req.Header.Set("Authorization", "Bearer valid")
	req.Header.Set("X-Loop-Runtime-Tenant", "tn-attacker")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	events := cap.captured()
	if len(events) != 1 || events[0].action != AuthFailureTenantNotFound {
		t.Fatalf("want 1 AuthFailureTenantNotFound event, got %+v", events)
	}
}

// -----------------------------------------------------------------------------
// AuthWithTTL — Service-auth path
// -----------------------------------------------------------------------------

func TestAuthFailureAudit_Wire_ServiceAuth_NonMemberTenant(t *testing.T) {
	cap := newCapturingDefault()

	authn := NewAuthenticator(Config{ServiceSecret: "valid-secret", Auditor: cap.auditor})
	handler := authn.MiddlewareWithTTL(
		&wireStubValidator{},
		&wireStubStore{memberships: []TenantMembership{{TenantID: "tn-real", Role: "owner"}}},
		time.Minute,
	)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	req.Header.Set("X-Service-Auth", "valid-secret")
	req.Header.Set("X-Loop-Runtime-User-ID", "u-1")
	req.Header.Set("X-Loop-Runtime-Tenant", "tn-attacker") // user is not in this tenant
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	events := cap.captured()
	if len(events) != 1 || events[0].action != AuthFailureTenantNotFound {
		t.Fatalf("want 1 AuthFailureTenantNotFound event, got %+v", events)
	}
}

func TestAuthFailureAudit_Wire_ServiceAuth_InvalidSecret(t *testing.T) {
	cap := newCapturingDefault()

	authn := NewAuthenticator(Config{ServiceSecret: "the-real-secret", Auditor: cap.auditor})
	handler := authn.MiddlewareWithTTL(
		&wireStubValidator{},
		&wireStubStore{},
		time.Minute,
	)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	req.Header.Set("X-Service-Auth", "wrong-secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	events := cap.captured()
	if len(events) != 1 || events[0].action != AuthFailureServiceMismatch {
		t.Fatalf("want 1 AuthFailureServiceMismatch event, got %+v", events)
	}
}

// -----------------------------------------------------------------------------
// Nil-safe: with no auditor configured, no panic and no test fakes called
// -----------------------------------------------------------------------------

func TestAuthFailureAudit_Wire_NilAuditor_NoPanic(t *testing.T) {
	authn := NewAuthenticator(Config{})
	handler := authn.RequireAdminService(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/y", nil)
	w := httptest.NewRecorder()
	// Should NOT panic
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("rejection should still happen even with nil auditor: got %d", w.Code)
	}
}
