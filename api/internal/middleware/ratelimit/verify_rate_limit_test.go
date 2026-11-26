// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package ratelimit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/go-chi/chi/v5"
)

// T1.1 of plan m3-verify-rate-limit-middleware (v1.1 SHIPPABLE 90.4).
// TDD-first tests for VerifyRateLimiter + Middleware() factory keyed by
// (tenant, domain) per the LOCKED j11-domains rate-limit ADR.
//
// EC-1 absorption (v1.1): tests use chi.NewRouter() + mounted route, NOT
// httptest.NewRequest wired directly to  chi sets URL params
// only when the route is mounted on chi router.

// injectUserContext builds a request context with a auth.UserContext for tests.
func injectUserContext(ctx context.Context, tenantID, userID string) context.Context {
	uc := &auth.UserContext{
		TenantID: tenantID,
		UserID:   userID,
	}
	return context.WithValue(ctx, auth.UserContextKeyPublic(), uc)
}

func mountRouter(t *testing.T, vrl *VerifyRateLimiter, downstreamStatus int) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.With(vrl.Middleware()).Post("/api/v1/domains/{domain}/verify",
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(downstreamStatus)
		})
	return r
}

// Test 1: First request within window allowed.
func TestVerifyRateLimit_AllowsFirstRequestPer30sWindow(t *testing.T) {
	vrl := NewVerifyRateLimiter(1, 30*time.Second)

	h := mountRouter(t, vrl, http.StatusAccepted)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains/example.com/verify", nil)
	req = req.WithContext(injectUserContext(req.Context(), "tenant-A", "user-X"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202 on first call, got %d", rr.Code)
	}
}

// Test 2: Second request within window rejected with Retry-After.
func TestVerifyRateLimit_Rejects2ndRequestWithRetryAfter(t *testing.T) {
	vrl := NewVerifyRateLimiter(1, 30*time.Second)

	h := mountRouter(t, vrl, http.StatusAccepted)
	ctx := injectUserContext(context.Background(), "tenant-A", "user-X")

	// 1st call — should pass
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/domains/example.com/verify", nil).WithContext(ctx)
	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusAccepted {
		t.Fatalf("expected 202 on first call, got %d", rr1.Code)
	}

	// 2nd call — should be rate-limited
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/domains/example.com/verify", nil).WithContext(ctx)
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on second call, got %d", rr2.Code)
	}
	if rr2.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on 429")
	}
	if !strings.Contains(rr2.Body.String(), "RATE_LIMITED") {
		t.Fatalf("expected RATE_LIMITED in body, got %q", rr2.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr2.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if body["code"] != "RATE_LIMITED" {
		t.Fatalf("expected code RATE_LIMITED, got %v", body["code"])
	}
}

// Test 3: Different domain on same tenant allowed (per-(tenant,domain) isolation).
func TestVerifyRateLimit_AllowsDifferentDomainSameTenant(t *testing.T) {
	vrl := NewVerifyRateLimiter(1, 30*time.Second)

	h := mountRouter(t, vrl, http.StatusAccepted)
	ctx := injectUserContext(context.Background(), "tenant-A", "user-X")

	// a.com — should pass
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/domains/a.com/verify", nil).WithContext(ctx)
	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusAccepted {
		t.Fatalf("a.com call: expected 202, got %d", rr1.Code)
	}

	// b.com — different domain, should also pass (per-(tenant,domain) keying)
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/domains/b.com/verify", nil).WithContext(ctx)
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusAccepted {
		t.Fatalf("b.com call: expected 202 (different domain), got %d", rr2.Code)
	}
}

// Test 4: Missing auth.UserContext → fail-open (ADR D3).
func TestVerifyRateLimit_FailsOpenOnMissingUserContext(t *testing.T) {
	vrl := NewVerifyRateLimiter(1, 30*time.Second)

	h := mountRouter(t, vrl, http.StatusAccepted)
	// NO injectUserContext — request comes without auth.UserContext
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains/example.com/verify", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("fail-open: expected 202, got %d", rr.Code)
	}
}

// Test 5 (EC-4 absorption v1.1): empty TenantID → fail-open (cross-tenant collision defense).
func TestVerifyRateLimit_FailOpen_EmptyTenantID(t *testing.T) {
	vrl := NewVerifyRateLimiter(1, 30*time.Second)

	h := mountRouter(t, vrl, http.StatusAccepted)
	ctx := injectUserContext(context.Background(), "", "user-X") // TenantID=""

	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains/example.com/verify", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("fail-open on empty TenantID: expected 202, got %d", rr.Code)
	}
}
