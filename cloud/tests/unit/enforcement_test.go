// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/loopzero/loopzero/cloud/internal/enforcement"
)

func TestPlanLimiterNilDB_AllowsEverything(t *testing.T) {
	limiter := enforcement.NewPlanLimiter(nil, nil)

	result := limiter.CheckLimit("tn-acme", "projects", 100)
	if !result.Allowed {
		t.Error("nil DB limiter should allow everything")
	}
	if result.Plan != "dev" {
		t.Errorf("plan = %q, want dev", result.Plan)
	}
}

func TestPlanLimiterNilDB_BuildTimeout(t *testing.T) {
	limiter := enforcement.NewPlanLimiter(nil, nil)
	timeout := limiter.GetBuildTimeout("tn-acme")
	if timeout != 1800 {
		t.Errorf("timeout = %d, want 1800", timeout)
	}
}

func TestPlanLimiterNilDB_RateLimit(t *testing.T) {
	limiter := enforcement.NewPlanLimiter(nil, nil)
	rate := limiter.GetRateLimit("tn-acme")
	if rate != 300 {
		t.Errorf("rate = %d, want 300", rate)
	}
}

func TestFormatLimitError(t *testing.T) {
	result := enforcement.LimitResult{
		Plan:    "starter",
		Metric:  "projects",
		Current: 2,
		Limit:   2,
	}

	errMap := enforcement.FormatLimitError(result, "https://app.loopzero.example/billing")

	if errMap["code"] != "PLAN_LIMIT_EXCEEDED" {
		t.Errorf("code = %v, want PLAN_LIMIT_EXCEEDED", errMap["code"])
	}
	errMsg, ok := errMap["error"].(string)
	if !ok {
		t.Fatal("error should be string")
	}
	if errMsg == "" {
		t.Error("error message should not be empty")
	}
	if errMap["plan"] != "starter" {
		t.Errorf("plan = %v, want starter", errMap["plan"])
	}
	if errMap["upgrade_url"] != "https://app.loopzero.example/billing" {
		t.Errorf("upgrade_url wrong")
	}
	// No internal terms
	for _, term := range []string{"Stripe", "stripe", "subscription", "invoice"} {
		if contains(errMsg, term) {
			t.Errorf("error message contains internal term %q: %s", term, errMsg)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestMiddleware_AllowedPassesThrough(t *testing.T) {
	limiter := enforcement.NewPlanLimiter(nil, nil) // nil DB = allow all

	check := enforcement.OperationCheck{
		Metric:  "projects",
		CountFn: func(tenantID string) int { return 1 },
	}

	handler := enforcement.Middleware(
		limiter, check,
		func(r *http.Request) string { return "tn-acme" },
		"https://app.loopzero.example/billing",
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest("POST", "/projects", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", w.Code)
	}
}

func TestMiddleware_NoTenant_PassesThrough(t *testing.T) {
	limiter := enforcement.NewPlanLimiter(nil, nil)

	check := enforcement.OperationCheck{
		Metric:  "projects",
		CountFn: func(tenantID string) int { return 999 },
	}

	handler := enforcement.Middleware(
		limiter, check,
		func(r *http.Request) string { return "" }, // No tenant
		"https://app.loopzero.example/billing",
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/projects", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (no tenant = pass through)", w.Code)
	}
}

func TestLimitResult_402ResponseShape(t *testing.T) {
	result := enforcement.LimitResult{
		Plan:    "starter",
		Metric:  "builds_per_month",
		Current: 100,
		Limit:   100,
	}

	errBody := enforcement.FormatLimitError(result, "https://app.loopzero.example/billing")

	// Verify JSON shape matches spec
	data, err := json.Marshal(errBody)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	requiredFields := []string{"code", "error", "plan", "limit", "current", "max", "upgrade_url"}
	for _, field := range requiredFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("missing required field %q in 402 response", field)
		}
	}
}
