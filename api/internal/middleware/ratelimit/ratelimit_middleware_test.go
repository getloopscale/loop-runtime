// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Split out of the flat `middleware` package's middleware_test.go (B-059): a
// test file lives in the package of its subject, and that file tested six
// middlewares that now live in five packages.

package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Rate limiter tests ---

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	rl := NewRateLimiter(5, time.Minute)

	for i := 0; i < 5; i++ {
		if !rl.Allow("tenant-1") {
			t.Errorf("request %d should be allowed under limit", i+1)
		}
	}
}

func TestRateLimiter_RejectsOverLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)

	for i := 0; i < 3; i++ {
		rl.Allow("tenant-1")
	}

	if rl.Allow("tenant-1") {
		t.Error("4th request should be rejected (limit is 3)")
	}
}

func TestRateLimiter_Middleware_Returns429(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)

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

// Retry-After comes from the limiter's own window, not a constant: a limiter
// configured for 10s must not tell clients to wait 60s.
func TestRateLimiter_Middleware_RetryAfterFollowsTheConfiguredWindow(t *testing.T) {
	rl := NewRateLimiter(1, 10*time.Second)
	handler := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/test", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/test", nil))

	if got := rec.Header().Get("Retry-After"); got != "10" {
		t.Errorf("Retry-After = %q, want \"10\" for a 10s window", got)
	}
}
