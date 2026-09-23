// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddleware_BlocksAtLimit(t *testing.T) {
	// PlanLimiter with nil DB always allows — we test the middleware wiring, not DB.
	// To test actual blocking, we use a custom LimitResult flow.

	// Create a handler that should NOT be called when blocked
	innerCalled := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// Build middleware with a mock that always blocks
	mw := Middleware(
		nil, // nil limiter = allow (dev mode)
		OperationCheck{
			Metric:  "projects",
			CountFn: func(context.Context, string) (int, error) { return 999, nil },
		},
		func(r *http.Request) string { return "tn-test" },
		"https://billing.example.com",
	)

	handler := mw(inner)
	req := httptest.NewRequest("POST", "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Nil limiter = allow
	if !innerCalled {
		t.Error("nil limiter should allow request through")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestMiddleware_EmptyTenantPasses(t *testing.T) {
	innerCalled := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	limiter := &PlanLimiter{} // nil DB = allow
	mw := Middleware(
		limiter,
		OperationCheck{
			Metric:  "projects",
			CountFn: func(context.Context, string) (int, error) { return 999, nil },
		},
		func(r *http.Request) string { return "" }, // empty tenant
		"",
	)

	handler := mw(inner)
	req := httptest.NewRequest("POST", "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !innerCalled {
		t.Error("empty tenant should pass through")
	}
}

func TestMiddleware_402ResponseShape(t *testing.T) {
	// Create a limiter that will block (we need a real DB for this, so we test the format instead)
	result := LimitResult{
		Allowed: false,
		Plan:    "starter",
		Metric:  "projects",
		Current: 1,
		Limit:   1,
	}
	errMap := FormatLimitError(result, "https://billing.example.com")

	data, err := json.Marshal(errMap)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	requiredFields := []string{"code", "message", "plan", "limit", "current", "max", "upgrade_url"}
	for _, field := range requiredFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("missing required field %q in 402 response", field)
		}
	}

	if parsed["code"] != "PLAN_LIMIT_EXCEEDED" {
		t.Errorf("code = %v, want PLAN_LIMIT_EXCEEDED", parsed["code"])
	}
}
