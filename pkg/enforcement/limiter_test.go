// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"net/http"
	"testing"
)

func TestCheckLimit_NilLimiter(t *testing.T) {
	var limiter *PlanLimiter
	result := limiter.CheckLimit("tn-test", "projects", 5)
	if !result.Allowed {
		t.Error("nil limiter should allow everything")
	}
	if result.Plan != "dev" {
		t.Errorf("nil limiter plan should be 'dev', got %q", result.Plan)
	}
}

func TestCheckLimit_NilDB(t *testing.T) {
	limiter := &PlanLimiter{db: nil}
	result := limiter.CheckLimit("tn-test", "projects", 5)
	if !result.Allowed {
		t.Error("nil DB should allow everything")
	}
}

func TestFormatLimitError_Fields(t *testing.T) {
	result := LimitResult{
		Allowed: false,
		Plan:    "starter",
		Metric:  "projects",
		Current: 1,
		Limit:   1,
	}
	errMap := FormatLimitError(result, "https://example.com/billing")

	if errMap["code"] != "PLAN_LIMIT_EXCEEDED" {
		t.Errorf("code = %v, want PLAN_LIMIT_EXCEEDED", errMap["code"])
	}
	msg, ok := errMap["message"].(string)
	if !ok || msg == "" {
		t.Error("message should be non-empty string")
	}
	if errMap["plan"] != "starter" {
		t.Errorf("plan = %v, want starter", errMap["plan"])
	}
	if errMap["current"] != 1 {
		t.Errorf("current = %v, want 1", errMap["current"])
	}
	if errMap["max"] != 1 {
		t.Errorf("max = %v, want 1", errMap["max"])
	}
	if errMap["upgrade_url"] != "https://example.com/billing" {
		t.Errorf("upgrade_url wrong")
	}

	// No internal terms in message
	for _, term := range []string{"ArgoCD", "Gitea", "MinIO", "CRD", "tenant_id"} {
		if contains(msg, term) {
			t.Errorf("message contains internal term %q: %s", term, msg)
		}
	}
}

func TestFormatLimitError_DisplayNames(t *testing.T) {
	tests := []struct {
		plan   string
		metric string
		want   string
	}{
		{"starter", "projects", "Starter"},
		{"pro", "builds_per_month", "Pro"},
		{"team", "seats", "Team"},
	}
	for _, tt := range tests {
		result := LimitResult{Plan: tt.plan, Metric: tt.metric, Limit: 1, Current: 1}
		errMap := FormatLimitError(result, "")
		msg := errMap["message"].(string)
		if !contains(msg, tt.want) {
			t.Errorf("message for plan %q should contain %q: %s", tt.plan, tt.want, msg)
		}
	}
}

func TestMiddleware_NilLimiter_Passes(t *testing.T) {
	// When PlanLimiter is nil (dev mode), middleware should pass all requests
	mw := Middleware(nil, OperationCheck{
		Metric:  "projects",
		CountFn: func(tenantID string) int { return 999 },
	}, func(r *http.Request) string { return "tn-test" }, "")

	if mw == nil {
		t.Error("middleware should not be nil")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
