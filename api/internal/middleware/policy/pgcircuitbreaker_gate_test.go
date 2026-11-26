// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Split out of the flat `middleware` package's middleware_test.go (B-059): a
// test file lives in the package of its subject, and that file tested six
// middlewares that now live in five packages.

package policy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- PG Circuit Breaker: no gate configured ---

// This was `ZeroTotal_PassesThrough`, guarding the division by zero of the
// percentage this middleware used to compute. Since B-054 the gate with
// hysteresis measures saturation, and the degenerate case left here is another:
// no gate configured. The property is the same — a degenerate state must not
// block traffic — in the place where it now lives.
func TestPGCircuitBreaker_NoGate_LetsRequestsThrough(t *testing.T) {
	cb := NewPGCircuitBreaker(PGCircuitBreakerConfig{})

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
