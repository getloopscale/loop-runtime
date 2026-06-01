// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/loopzero/loopzero/cloud/internal/ratelimit"
)

// T6.3 — the rate-limit middleware allows up to the limit, then returns 429.
func TestRateLimitMiddleware_BlocksOverLimit(t *testing.T) {
	l := ratelimit.NewInMemoryLimiter(2, time.Minute)
	keyFn := func(*http.Request) string { return "same-key" }
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := ratelimit.Middleware(l, 2, keyFn)(next)

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/x", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/x", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after limit, got %d", rec.Code)
	}
}

// T6.3 — different keys are limited independently (per-IP / per-user isolation).
func TestRateLimitMiddleware_IndependentKeys(t *testing.T) {
	l := ratelimit.NewInMemoryLimiter(1, time.Minute)
	var key string
	keyFn := func(*http.Request) string { return key }
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := ratelimit.Middleware(l, 1, keyFn)(next)

	key = "user-a"
	recA := httptest.NewRecorder()
	h.ServeHTTP(recA, httptest.NewRequest("POST", "/x", nil))

	key = "user-b" // different key — must still be allowed
	recB := httptest.NewRecorder()
	h.ServeHTTP(recB, httptest.NewRequest("POST", "/x", nil))
	if recB.Code != http.StatusOK {
		t.Fatalf("independent key should be allowed, got %d", recB.Code)
	}
}
