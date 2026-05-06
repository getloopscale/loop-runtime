// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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
	handler := AuthWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := AuthWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := AuthWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := AuthWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := AuthWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	// Act: no X-LoopZero-Tenant header
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
	handler := AuthWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = GetUserContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	// Act
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("X-LoopZero-Tenant", "tn-b")
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
	oldSecret := ServiceAuthSecret
	ServiceAuthSecret = "test-service-secret"
	defer func() { ServiceAuthSecret = oldSecret }()

	store := &stubMembershipStore{
		memberships: []TenantMembership{{TenantID: "tn-svc", Role: "admin"}},
	}
	validator := &stubValidator{} // should not be called

	var captured *UserContext
	handler := AuthWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = GetUserContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	// Act
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Service-Auth", "test-service-secret")
	req.Header.Set("X-LoopZero-User-ID", "svc-user")
	req.Header.Set("X-LoopZero-Tenant", "tn-svc")
	req.Header.Set("X-LoopZero-Email", "svc@test.com")
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
	oldSecret := ServiceAuthSecret
	ServiceAuthSecret = "correct-secret"
	defer func() { ServiceAuthSecret = oldSecret }()

	store := &stubMembershipStore{}
	validator := &stubValidator{}
	handler := AuthWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := AuthWithTTL(validator, store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

// --- Idempotency middleware tests ---

type inMemoryIdempotencyStore struct {
	records map[string]*IdempotencyRecord
}

func newInMemoryIdempotencyStore() *inMemoryIdempotencyStore {
	return &inMemoryIdempotencyStore{records: make(map[string]*IdempotencyRecord)}
}

func (s *inMemoryIdempotencyStore) Get(_ context.Context, key string, tenantID string) (*IdempotencyRecord, error) {
	rec, ok := s.records[tenantID+":"+key]
	if !ok {
		return nil, nil
	}
	return rec, nil
}

func (s *inMemoryIdempotencyStore) Store(_ context.Context, rec *IdempotencyRecord) error {
	s.records[rec.TenantID+":"+rec.Key] = rec
	return nil
}

func TestIdempotency_MissingKey_Returns400(t *testing.T) {
	// Arrange
	store := newInMemoryIdempotencyStore()
	handler := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	// Act
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}

	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "MISSING_IDEMPOTENCY_KEY" {
		t.Errorf("expected MISSING_IDEMPOTENCY_KEY code, got %q", body["code"])
	}
}

func TestIdempotency_NewKey_ProcessesRequest(t *testing.T) {
	// Arrange
	store := newInMemoryIdempotencyStore()
	called := false
	handler := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"build_id":"bld-1"}`))
	}))

	// Act
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(`{"project":"my-app"}`))
	req.Header.Set("Idempotency-Key", "key-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Assert
	if !called {
		t.Error("handler should have been called for new key")
	}
	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rec.Code)
	}
}

func TestIdempotency_SameKeySamePayload_ReturnsCached(t *testing.T) {
	// Arrange
	store := newInMemoryIdempotencyStore()
	callCount := 0
	handler := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"build_id":"bld-1"}`))
	}))

	payload := `{"project":"my-app"}`

	// First call
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(payload))
	req1.Header.Set("Idempotency-Key", "key-2")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Second call with same key and payload
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(payload))
	req2.Header.Set("Idempotency-Key", "key-2")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	// Assert
	if callCount != 1 {
		t.Errorf("handler should have been called once, got %d", callCount)
	}
	if rec2.Code != http.StatusAccepted {
		t.Errorf("cached replay should return 202, got %d", rec2.Code)
	}
}

func TestIdempotency_SameKeyDifferentPayload_Returns422(t *testing.T) {
	// Arrange
	store := newInMemoryIdempotencyStore()
	handler := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"build_id":"bld-1"}`))
	}))

	// First call
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(`{"project":"app-1"}`))
	req1.Header.Set("Idempotency-Key", "key-3")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Second call: same key, different payload
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(`{"project":"app-2"}`))
	req2.Header.Set("Idempotency-Key", "key-3")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	// Assert
	if rec2.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", rec2.Code)
	}

	var body map[string]string
	_ = json.Unmarshal(rec2.Body.Bytes(), &body)
	if body["code"] != "IDEMPOTENCY_MISMATCH" {
		t.Errorf("expected IDEMPOTENCY_MISMATCH code, got %q", body["code"])
	}
}

// --- Rate limiter tests ---

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	rl := NewRateLimiter(5, time.Minute)
	defer rl.Stop()

	for i := 0; i < 5; i++ {
		if !rl.Allow("tenant-1") {
			t.Errorf("request %d should be allowed under limit", i+1)
		}
	}
}

func TestRateLimiter_RejectsOverLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)
	defer rl.Stop()

	for i := 0; i < 3; i++ {
		rl.Allow("tenant-1")
	}

	if rl.Allow("tenant-1") {
		t.Error("4th request should be rejected (limit is 3)")
	}
}

func TestRateLimiter_Middleware_Returns429(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	defer rl.Stop()

	handler := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request passes
	req1 := httptest.NewRequest(http.MethodPost, "/test", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Errorf("first request: expected 200, got %d", rec1.Code)
	}

	// Second request rate-limited
	req2 := httptest.NewRequest(http.MethodPost, "/test", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request: expected 429, got %d", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") == "" {
		t.Error("429 response should include Retry-After header")
	}
}

func TestRateLimiter_DifferentTenants_IndependentLimits(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	defer rl.Stop()

	if !rl.Allow("tenant-a") {
		t.Error("tenant-a first request should pass")
	}
	if rl.Allow("tenant-a") {
		t.Error("tenant-a second request should be blocked")
	}
	if !rl.Allow("tenant-b") {
		t.Error("tenant-b should have independent limit")
	}
}

// --- Correlation ID middleware tests ---

func TestCorrelationID_GeneratesWhenMissing(t *testing.T) {
	handler := CorrelationID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("Correlation-ID")
		if id == "" {
			t.Error("Correlation-ID should be generated and set on request")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Correlation-ID") == "" {
		t.Error("Correlation-ID should be set on response")
	}
}

func TestCorrelationID_PreservesExisting(t *testing.T) {
	handler := CorrelationID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Correlation-ID") != "my-corr-id" {
			t.Errorf("expected preserved Correlation-ID, got %q", r.Header.Get("Correlation-ID"))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Correlation-ID", "my-corr-id")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Correlation-ID") != "my-corr-id" {
		t.Errorf("response Correlation-ID = %q, want my-corr-id", rec.Header().Get("Correlation-ID"))
	}
}

// --- PG Circuit Breaker: zero total connections ---

func TestPGCircuitBreaker_ZeroTotal_PassesThrough(t *testing.T) {
	cb := NewPGCircuitBreaker(PGCircuitBreakerConfig{
		MaxConns:      25,
		ThresholdPct:  90,
		PoolStatsFunc: func() (inUse int32, total int32) { return 0, 0 },
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := cb.Middleware()(handler)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("POST with 0/0 pool: got %d, want 200", w.Code)
	}
}

// --- Roles middleware tests ---

func TestRequireRole_AdminRequired_OwnerPasses(t *testing.T) {
	handler := RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	ctx := context.WithValue(req.Context(), userContextKey, &UserContext{
		UserID: "u-1", TenantID: "tn-1", Role: "owner",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code != http.StatusOK {
		t.Errorf("owner should pass admin check, got %d", rec.Code)
	}
}

func TestRequireRole_AdminRequired_MemberFails(t *testing.T) {
	handler := RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	ctx := context.WithValue(req.Context(), userContextKey, &UserContext{
		UserID: "u-1", TenantID: "tn-1", Role: "member",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code != http.StatusForbidden {
		t.Errorf("member should fail admin check, got %d", rec.Code)
	}
}

func TestRequireRole_NoUserContext_Returns401(t *testing.T) {
	handler := RequireRole("member")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without user context, got %d", rec.Code)
	}
}

func TestHasMinRole(t *testing.T) {
	tests := []struct {
		user, min string
		want      bool
	}{
		{"owner", "owner", true},
		{"owner", "admin", true},
		{"owner", "member", true},
		{"admin", "owner", false},
		{"admin", "admin", true},
		{"admin", "member", true},
		{"member", "admin", false},
		{"member", "member", true},
	}
	for _, tt := range tests {
		got := HasMinRole(tt.user, tt.min)
		if got != tt.want {
			t.Errorf("HasMinRole(%q, %q) = %v, want %v", tt.user, tt.min, got, tt.want)
		}
	}
}

// --- Audit middleware tests ---

func TestAuditLog_SkipsGET(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	writerCalled := false
	writer := AuditWriter(func(_, _, _, _, _, _ string, _ map[string]any) {
		writerCalled = true
	})

	handler := AuditLog(logger, writer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if writerCalled {
		t.Error("audit writer should not be called for GET requests")
	}
}

func TestAuditLog_LogsMutationWithContext(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	var capturedAction string
	writer := AuditWriter(func(tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) {
		capturedAction = action
	})

	handler := AuditLog(logger, writer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-app", nil)
	ctx := context.WithValue(req.Context(), userContextKey, &UserContext{
		UserID: "u-1", TenantID: "tn-1", Role: "owner",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))

	if capturedAction == "" {
		t.Error("audit writer should be called for POST requests with tenant context")
	}
}

// CHG-2026-05-06-AUDIT-RESOURCE-ID: POST /api/v1/builds carries the
// project identifier in the request body (not the URL path). The audit
// middleware must snoop the body and populate resource_id with project_id
// so audit forensics can join build entries to projects.
func TestAuditLog_EnrichResourceID_BuildSubmitted(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	var capturedResourceID, capturedAction, capturedResourceType string
	var handlerSawBody string
	writer := AuditWriter(func(_, action, _, resourceType, resourceID, _ string, _ map[string]any) {
		capturedAction = action
		capturedResourceType = resourceType
		capturedResourceID = resourceID
	})

	body := `{"project_id":"my-monorepo","environment":"production","commit_sha":"abc123"}`
	handler := AuditLog(logger, writer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the body was restored — handlers MUST see the same bytes
		// the client sent.
		buf, _ := io.ReadAll(r.Body)
		handlerSawBody = string(buf)
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(body))
	ctx := context.WithValue(req.Context(), userContextKey, &UserContext{
		UserID: "u-1", TenantID: "tn-1", Role: "owner",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))

	if capturedAction != "build.submitted" {
		t.Errorf("action = %q, want %q", capturedAction, "build.submitted")
	}
	if capturedResourceType != "build" {
		t.Errorf("resource_type = %q, want %q", capturedResourceType, "build")
	}
	if capturedResourceID != "my-monorepo" {
		t.Errorf("resource_id = %q, want %q (project_id from body)",
			capturedResourceID, "my-monorepo")
	}
	if handlerSawBody != body {
		t.Errorf("handler saw mutated body\n got: %q\nwant: %q", handlerSawBody, body)
	}
}

// POST /api/v1/secrets carries project as ?project= query — middleware
// must consult both query and body to populate resource_id.
func TestAuditLog_EnrichResourceID_SecretSet_Query(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	var capturedResourceID, capturedAction string
	writer := AuditWriter(func(_, action, _, _, resourceID, _ string, _ map[string]any) {
		capturedAction = action
		capturedResourceID = resourceID
	})

	body := `{"name":"DB_URL","value":"redacted","environment":"production","app":"api"}`
	handler := AuditLog(logger, writer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets?project=my-app", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(body))
	ctx := context.WithValue(req.Context(), userContextKey, &UserContext{
		UserID: "u-1", TenantID: "tn-1", Role: "owner",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))

	if capturedAction != "secret.set" {
		t.Errorf("action = %q, want secret.set", capturedAction)
	}
	if capturedResourceID != "my-app" {
		t.Errorf("resource_id = %q, want my-app (project from query)",
			capturedResourceID)
	}
}

// POST /api/v1/domains body uses "project" key (not "project_id").
func TestAuditLog_EnrichResourceID_DomainAdded(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	var capturedResourceID string
	writer := AuditWriter(func(_, _, _, _, resourceID, _ string, _ map[string]any) {
		capturedResourceID = resourceID
	})

	body := `{"domain":"example.com","project":"web","app":"frontend","environment":"production"}`
	handler := AuditLog(logger, writer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(body))
	ctx := context.WithValue(req.Context(), userContextKey, &UserContext{
		UserID: "u-1", TenantID: "tn-1", Role: "owner",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))

	if capturedResourceID != "web" {
		t.Errorf("resource_id = %q, want web (project from body)", capturedResourceID)
	}
}

// Malformed JSON body must not crash the middleware nor corrupt the
// downstream handler — resource_id stays "" and the handler still sees
// the original (broken) bytes for its own error reporting.
func TestAuditLog_EnrichResourceID_MalformedBody(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	var capturedResourceID string
	writer := AuditWriter(func(_, _, _, _, resourceID, _ string, _ map[string]any) {
		capturedResourceID = resourceID
	})

	body := `{not valid json`
	var handlerSaw string
	handler := AuditLog(logger, writer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		handlerSaw = string(buf)
		w.WriteHeader(http.StatusBadRequest)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(body))
	ctx := context.WithValue(req.Context(), userContextKey, &UserContext{
		UserID: "u-1", TenantID: "tn-1", Role: "owner",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))

	if capturedResourceID != "" {
		t.Errorf("resource_id = %q, want empty for malformed body", capturedResourceID)
	}
	if handlerSaw != body {
		t.Errorf("handler saw %q, want %q (body restoration)", handlerSaw, body)
	}
}

// --- Audit actions tests ---

func TestDeriveAction(t *testing.T) {
	tests := []struct {
		method, path, want string
	}{
		{"POST", "/api/v1/projects", "project.created"},
		{"POST", "/api/v1/builds", "build.submitted"},
		{"POST", "/api/v1/builds/123/cancel", "build.cancelled"},
		{"DELETE", "/api/v1/projects/my-app", "project.deleted"},
		{"DELETE", "/api/v1/secrets/DB_URL", "secret.deleted"},
		{"PATCH", "/api/v1/teams/members/paulo", "member.role_changed"},
	}
	for _, tt := range tests {
		got := deriveAction(tt.method, tt.path)
		if got != tt.want {
			t.Errorf("deriveAction(%q, %q) = %q, want %q", tt.method, tt.path, got, tt.want)
		}
	}
}

func TestExtractResourceType(t *testing.T) {
	tests := []struct {
		path, want string
	}{
		{"/api/v1/projects/my-app", "project"},
		{"/api/v1/builds/123", "build"},
		{"/api/v1/secrets/DB_URL", "secret"},
	}
	for _, tt := range tests {
		got := extractResourceType(tt.path)
		if got != tt.want {
			t.Errorf("extractResourceType(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestExtractResourceID(t *testing.T) {
	tests := []struct {
		path, want string
	}{
		{"/api/v1/projects/my-app", "my-app"},
		{"/api/v1/secrets/DB_URL", "DB_URL"},
		{"/api/v1/teams/members/paulo", "paulo"},
	}
	for _, tt := range tests {
		got := extractResourceID(tt.path)
		if got != tt.want {
			t.Errorf("extractResourceID(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// --- Request Logging middleware tests ---

func TestRequestLogging_NilLogger_NoPanic(t *testing.T) {
	handler := RequestLogging(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req) // should not panic

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// --- Service auth validation tests ---

func TestValidateServiceAuth_DualKeyRotation(t *testing.T) {
	oldSecret := ServiceAuthSecret
	oldPrev := ServiceAuthSecretPrevious
	defer func() {
		ServiceAuthSecret = oldSecret
		ServiceAuthSecretPrevious = oldPrev
	}()

	ServiceAuthSecret = "new-secret"
	ServiceAuthSecretPrevious = "old-secret"

	if !validateServiceAuth("new-secret") {
		t.Error("current secret should be accepted")
	}
	if !validateServiceAuth("old-secret") {
		t.Error("previous secret should be accepted during rotation")
	}
	if validateServiceAuth("wrong-secret") {
		t.Error("invalid secret should be rejected")
	}
}

func TestValidateServiceAuth_EmptyConfig(t *testing.T) {
	oldSecret := ServiceAuthSecret
	defer func() { ServiceAuthSecret = oldSecret }()

	ServiceAuthSecret = ""
	if validateServiceAuth("any-secret") {
		t.Error("should reject when service auth is not configured")
	}
}

// --- Token cache key security ---

func TestTokenCacheKey_DeterministicHash(t *testing.T) {
	key1 := tokenCacheKey("my-secret-token")
	key2 := tokenCacheKey("my-secret-token")
	if key1 != key2 {
		t.Error("same token should produce same cache key")
	}

	key3 := tokenCacheKey("different-token")
	if key1 == key3 {
		t.Error("different tokens should produce different cache keys")
	}

	// Should not contain the original token (SEC-001)
	if strings.Contains(key1, "my-secret-token") {
		t.Error("cache key should be a hash, not contain the raw token")
	}
}
