// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package policy

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	planpkg "github.com/getloopscale/contracts/plan"
	"github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/pkg/enforcement"
)

// fakeCostStore implements enforcement.CostStore in-memory.
type fakeCostStore struct {
	counter planpkg.CostCounter
	found   bool
	err     error
}

func (f *fakeCostStore) GetCounter(_ context.Context, _ string, _ planpkg.CostMetric) (planpkg.CostCounter, bool, error) {
	return f.counter, f.found, f.err
}

// stubPlanReader returns a static plan for any tenant.
type stubPlanReader struct {
	plan planpkg.Plan
	err  error
}

func (s *stubPlanReader) GetTenantPlan(_ context.Context, _ string) (planpkg.Plan, error) {
	return s.plan, s.err
}

// helper: build a request with a auth.UserContext for tenant id.
func withTenant(tenantID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	uc := &auth.UserContext{
		UserID:   "u-1",
		TenantID: tenantID,
		Role:     "owner",
	}
	ctx := context.WithValue(req.Context(), auth.UserContextKeyPublic(), uc)
	return req.WithContext(ctx)
}

// -----------------------------------------------------------------------------
// TestEnforcement_RejectOnExceeded — canonical T1.9 TDD RED case
// -----------------------------------------------------------------------------

func TestEnforcement_RejectOnExceeded(t *testing.T) {
	store := &fakeCostStore{
		counter: planpkg.CostCounter{
			TenantID: "tn-1",
			Plan:     planpkg.PlanStarter,
			Metric:   planpkg.MetricBuildMinutes,
			Value:    101, // over Starter's 100 cap
		},
		found: true,
	}
	checker := enforcement.NewCapChecker(store)
	plans := &stubPlanReader{plan: planpkg.PlanStarter}
	enforcer := NewCostEnforcer(checker, plans, slog.Default())

	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})
	handler := enforcer.Middleware(planpkg.MetricBuildMinutes)(next)

	req := withTenant("tn-1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("want 402, got %d body=%s", w.Code, w.Body.String())
	}
	if called {
		t.Error("next handler must NOT be called on cap exceeded")
	}
}

func TestEnforcement_402ResponseShape(t *testing.T) {
	store := &fakeCostStore{
		counter: planpkg.CostCounter{
			TenantID:    "tn-1",
			Plan:        planpkg.PlanStarter,
			Metric:      planpkg.MetricEgressBytes,
			Value:       200 * 1024 * 1024 * 1024,
			PeriodStart: 1714521600,
		},
		found: true,
	}
	checker := enforcement.NewCapChecker(store)
	plans := &stubPlanReader{plan: planpkg.PlanStarter}
	enforcer := NewCostEnforcer(checker, plans, slog.Default())

	handler := enforcer.Middleware(planpkg.MetricEgressBytes)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	req := withTenant("tn-1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body["code"] != "COST_CAP_EXCEEDED" {
		t.Errorf("want code COST_CAP_EXCEEDED, got %v", body["code"])
	}
	if body["metric"] != string(planpkg.MetricEgressBytes) {
		t.Errorf("want metric=egress_bytes, got %v", body["metric"])
	}
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, "egress") {
		t.Errorf("message should mention egress: %q", msg)
	}
	if !strings.Contains(msg, "Upgrade") {
		t.Errorf("message should suggest upgrade: %q", msg)
	}
	// Must NOT leak internal terms (Rule 3).
	for _, forbidden := range []string{"ArgoCD", "Gitea", "MinIO", "advisory lock", "tenant_id"} {
		if strings.Contains(msg, forbidden) {
			t.Errorf("message leaks internal term %q: %q", forbidden, msg)
		}
	}
}

func TestEnforcement_AllowsUnderCap(t *testing.T) {
	store := &fakeCostStore{
		counter: planpkg.CostCounter{
			TenantID: "tn-1",
			Plan:     planpkg.PlanStarter,
			Metric:   planpkg.MetricBuildMinutes,
			Value:    50, // under 100 cap
		},
		found: true,
	}
	checker := enforcement.NewCapChecker(store)
	plans := &stubPlanReader{plan: planpkg.PlanStarter}
	enforcer := NewCostEnforcer(checker, plans, slog.Default())

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	})
	handler := enforcer.Middleware(planpkg.MetricBuildMinutes)(next)

	req := withTenant("tn-1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("next handler must be called when under cap")
	}
	if w.Code != http.StatusAccepted {
		t.Errorf("want 202, got %d", w.Code)
	}
}

func TestEnforcement_NilEnforcer_PassThrough(t *testing.T) {
	var enforcer *CostEnforcer // explicitly nil
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})
	handler := enforcer.Middleware(planpkg.MetricBuildMinutes)(next)

	req := withTenant("tn-x")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("nil enforcer must be no-op (pass-through)")
	}
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
}

func TestEnforcement_PlanLookupFails_FailsOpen(t *testing.T) {
	store := &fakeCostStore{found: false} // doesn't matter; plan lookup fails first
	checker := enforcement.NewCapChecker(store)
	plans := &stubPlanReader{err: errors.New("db connection refused")}
	enforcer := NewCostEnforcer(checker, plans, slog.Default())

	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})
	handler := enforcer.Middleware(planpkg.MetricBuildMinutes)(next)

	req := withTenant("tn-1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("plan lookup failure must fail-open (next handler called)")
	}
}

func TestEnforcement_Enterprise_AlwaysAllowed(t *testing.T) {
	// Enterprise cap = Unlimited; even huge counter must pass.
	store := &fakeCostStore{
		counter: planpkg.CostCounter{
			TenantID: "tn-ent",
			Plan:     planpkg.PlanEnterprise,
			Metric:   planpkg.MetricEgressBytes,
			Value:    1024 * 1024 * 1024 * 1024 * 100, // 100 TB
		},
		found: true,
	}
	checker := enforcement.NewCapChecker(store)
	plans := &stubPlanReader{plan: planpkg.PlanEnterprise}
	enforcer := NewCostEnforcer(checker, plans, slog.Default())

	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})
	handler := enforcer.Middleware(planpkg.MetricEgressBytes)(next)

	req := withTenant("tn-ent")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("enterprise must always pass")
	}
}

func TestEnforcement_Unauthenticated_Returns401(t *testing.T) {
	store := &fakeCostStore{}
	checker := enforcement.NewCapChecker(store)
	plans := &stubPlanReader{plan: planpkg.PlanStarter}
	enforcer := NewCostEnforcer(checker, plans, slog.Default())

	handler := enforcer.Middleware(planpkg.MetricBuildMinutes)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	// No auth.UserContext attached.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

// EC-NEW-13b (T1.9 flavor) — plan downgrade message hint
func TestEnforcement_PlanDowngrade_AdditionalMessage(t *testing.T) {
	// Counter 1.5× cap or more triggers the downgrade hint.
	store := &fakeCostStore{
		counter: planpkg.CostCounter{
			TenantID: "tn-down",
			Plan:     planpkg.PlanStarter,
			Metric:   planpkg.MetricBuildMinutes,
			Value:    250, // 2.5× starter's 100 cap → suggests downgrade
		},
		found: true,
	}
	checker := enforcement.NewCapChecker(store)
	plans := &stubPlanReader{plan: planpkg.PlanStarter}
	enforcer := NewCostEnforcer(checker, plans, slog.Default())

	handler := enforcer.Middleware(planpkg.MetricBuildMinutes)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	req := withTenant("tn-down")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var body map[string]any
	_ = json.NewDecoder(w.Body).Decode(&body)
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, "recently changed plans") {
		t.Errorf("downgrade hint missing from message: %q", msg)
	}
}
