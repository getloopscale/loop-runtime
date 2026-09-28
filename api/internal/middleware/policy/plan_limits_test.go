// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package policy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/pkg/enforcement"
)

// planLimitsStore is an in-memory enforcement.LimitsStore.
type planLimitsStore struct {
	plan    string
	hard    int
	planErr error
}

func (s planLimitsStore) GetTenantPlan(context.Context, string) (string, error) {
	return s.plan, s.planErr
}

func (s planLimitsStore) GetLimits(context.Context, string, string) (int, int, error) {
	if s.hard == 0 {
		return 0, 0, sql.ErrNoRows
	}
	return s.hard, 0, nil
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func countOf(n int) enforcement.UsageCounter {
	return func(context.Context, string) (int, error) { return n, nil }
}

func serve(limiter *enforcement.PlanLimiter, check PlanLimitCheck, tenant string) (*httptest.ResponseRecorder, bool) {
	innerCalled := false
	mw := PlanLimits(limiter, check, func(*http.Request) string { return tenant }, "https://billing.example.com", quietLogger())
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusCreated)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil))
	return rec, innerCalled
}

func TestPlanLimits_NilLimiterPassesThrough(t *testing.T) {
	rec, inner := serve(nil, PlanLimitCheck{Metric: "seats", Count: countOf(999)}, "tn-a")

	if !inner || rec.Code != http.StatusCreated {
		t.Fatalf("nil limiter must pass through; code=%d inner=%v", rec.Code, inner)
	}
}

func TestPlanLimits_RequestWithoutTenantPassesThrough(t *testing.T) {
	limiter := enforcement.NewPlanLimiter(planLimitsStore{plan: "starter", hard: 1}, quietLogger())

	rec, inner := serve(limiter, PlanLimitCheck{Metric: "seats", Count: countOf(999)}, "")

	if !inner || rec.Code != http.StatusCreated {
		t.Fatalf("no tenant must pass through; code=%d inner=%v", rec.Code, inner)
	}
}

func TestPlanLimits_UnderTheLimitCallsTheHandler(t *testing.T) {
	limiter := enforcement.NewPlanLimiter(planLimitsStore{plan: "pro", hard: 3}, quietLogger())

	rec, inner := serve(limiter, PlanLimitCheck{Metric: "builds_per_month", Count: countOf(1)}, "tn-a")

	if !inner || rec.Code != http.StatusCreated {
		t.Fatalf("under the limit must pass; code=%d inner=%v", rec.Code, inner)
	}
}

func TestPlanLimits_AtTheLimitAnswers402WithTheContractBody(t *testing.T) {
	limiter := enforcement.NewPlanLimiter(planLimitsStore{plan: "starter", hard: 1}, quietLogger())

	rec, inner := serve(limiter, PlanLimitCheck{Metric: "builds_per_month", Count: countOf(1)}, "tn-a")

	if inner || rec.Code != http.StatusPaymentRequired {
		t.Fatalf("at the limit must answer 402 without the handler; code=%d inner=%v", rec.Code, inner)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 402 body: %v", err)
	}
	for _, field := range []string{"code", "message", "plan", "limit", "current", "max", "upgrade_url"} {
		if _, ok := body[field]; !ok {
			t.Errorf("402 body is missing contract field %q", field)
		}
	}
	if body["code"] != "PLAN_LIMIT_EXCEEDED" || body["upgrade_url"] != "https://billing.example.com" {
		t.Errorf("unexpected 402 body %v", body)
	}
}

func TestPlanLimits_Answers503WhenUsageCannotBeCounted(t *testing.T) {
	limiter := enforcement.NewPlanLimiter(planLimitsStore{plan: "team", hard: 10}, quietLogger())
	failing := func(context.Context, string) (int, error) { return 0, errors.New("connection reset") }

	rec, inner := serve(limiter, PlanLimitCheck{Metric: "seats", Count: failing}, "tn-a")

	if inner || rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("count failure must answer 503 without the handler; code=%d inner=%v", rec.Code, inner)
	}
	if !strings.Contains(rec.Body.String(), "PLATFORM_DEGRADED") {
		t.Errorf("503 body must carry PLATFORM_DEGRADED, got %s", rec.Body.String())
	}
}

func TestPlanLimits_Answers503WhenThePlanCannotBeResolved(t *testing.T) {
	limiter := enforcement.NewPlanLimiter(planLimitsStore{planErr: errors.New("connection reset")}, quietLogger())

	rec, inner := serve(limiter, PlanLimitCheck{Metric: "seats", Count: countOf(0)}, "tn-a")

	if inner || rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("plan failure must answer 503 without the handler; code=%d inner=%v", rec.Code, inner)
	}
}

func TestPlanLimitExceededBody_UsesProductWordingWithoutInternalTerms(t *testing.T) {
	cases := map[string]string{
		"projects":             "Enterprise allows 5 projects",
		"services_per_project": "Enterprise allows 5 services per project",
		"environments":         "Enterprise allows 5 environments",
		"builds_per_month":     "Enterprise allows 5 builds per month",
		"seats":                "Enterprise allows 5 team members",
		"secrets_per_env":      "Enterprise allows 5 secrets per environment",
		"unknown_metric":       "Enterprise allows 5 unknown_metric",
	}
	for metric, want := range cases {
		msg := planLimitExceededBody(enforcement.LimitResult{Plan: "enterprise", Metric: metric, Limit: 5, Current: 5}, "")["message"].(string)
		if !strings.Contains(msg, want) {
			t.Errorf("metric %q: message %q, want it to contain %q", metric, msg, want)
		}
		for _, term := range []string{"ArgoCD", "Gitea", "MinIO", "CRD", "tenant_id"} {
			if strings.Contains(msg, term) {
				t.Errorf("message contains internal term %q: %s", term, msg)
			}
		}
	}
}
