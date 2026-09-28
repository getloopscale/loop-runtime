// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Split out of the flat `middleware` package's auth_extended_test.go (B-059).

package ratelimit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/middleware/auth"
)

// --- Rate limiter extended tests ---

func TestRateLimiter_Middleware_UsesAuthenticatedTenantID(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)

	handlerCalls := 0
	handler := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalls++
		w.WriteHeader(http.StatusOK)
	}))

	// First request with tenant context
	req1 := httptest.NewRequest(http.MethodPost, "/test", nil)
	ctx1 := context.WithValue(req1.Context(), auth.UserContextKeyPublic(), &auth.UserContext{TenantID: "tn-rate"})
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1.WithContext(ctx1))

	if rec1.Code != http.StatusOK {
		t.Errorf("first request: expected 200, got %d", rec1.Code)
	}

	// Second request from same tenant - should be rate limited
	req2 := httptest.NewRequest(http.MethodPost, "/test", nil)
	ctx2 := context.WithValue(req2.Context(), auth.UserContextKeyPublic(), &auth.UserContext{TenantID: "tn-rate"})
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2.WithContext(ctx2))

	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request: expected 429, got %d", rec2.Code)
	}

	// Request from different tenant - should pass
	req3 := httptest.NewRequest(http.MethodPost, "/test", nil)
	ctx3 := context.WithValue(req3.Context(), auth.UserContextKeyPublic(), &auth.UserContext{TenantID: "tn-other"})
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3.WithContext(ctx3))

	if rec3.Code != http.StatusOK {
		t.Errorf("different tenant: expected 200, got %d", rec3.Code)
	}
}

func TestRateLimiter_429Response_HasJSON(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)

	// Exhaust the single allowed request
	handler := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest(http.MethodPost, "/test", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1) // first request passes

	// Second request should be rate limited with JSON body
	req2 := httptest.NewRequest(http.MethodPost, "/test", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec2.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec2.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body["code"] != "RATE_LIMITED" {
		t.Errorf("expected RATE_LIMITED code, got %q", body["code"])
	}
}
