// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package policy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPGCircuitBreaker_RejectAt90Percent verifies that when the PG pool
// reports >= 90% saturation, mutation requests receive 503 Service
// Unavailable, while read requests (GET) continue.
func TestPGCircuitBreaker_RejectAt90Percent(t *testing.T) {
	// Fake pool stats: 23 of 25 connections in use = 92%.
	cb := NewPGCircuitBreaker(PGCircuitBreakerConfig{
		Gate: fixedGate(false), // saturated pool: the gate is tripped
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := cb.Middleware()(handler)

	// POST (mutation) should be rejected.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("POST at 92%% pool: got %d, want 503", w.Code)
	}

	// GET (read) should pass through.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/builds", nil)
	w = httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET at 92%% pool: got %d, want 200", w.Code)
	}
}

// TestPGCircuitBreaker_AllowsBelowThreshold verifies that when the pool
// is below the threshold, all requests pass through normally.
func TestPGCircuitBreaker_AllowsBelowThreshold(t *testing.T) {
	cb := NewPGCircuitBreaker(PGCircuitBreakerConfig{
		Gate: fixedGate(true), // pool with headroom: the gate is open
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := cb.Middleware()(handler)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("POST at 60%% pool: got %d, want 200", w.Code)
	}
}

// TestPGCircuitBreaker_RetryAfterHeader verifies the 503 response
// includes a Retry-After header.
func TestPGCircuitBreaker_RetryAfterHeader(t *testing.T) {
	cb := NewPGCircuitBreaker(PGCircuitBreakerConfig{
		Gate: fixedGate(false), // saturated pool
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := cb.Middleware()(handler)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if ra := w.Header().Get("Retry-After"); ra == "" {
		t.Error("503 response missing Retry-After header")
	}
}

// fixedGate is a gate with a constant answer. B-054: saturation is no longer
// expressed as an instantaneous percentage in this middleware — what measures it,
// with hysteresis, is `buildplane/admission.PGPoolBreaker`, which now decides.
type fixedGate bool

func (g fixedGate) Allow() bool { return bool(g) }
