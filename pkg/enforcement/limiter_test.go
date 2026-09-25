// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// planLimitsFake returns an in-memory LimitsStore answering the canonical
// PlanLimiter probe pair: tenant → plan, and (plan, metric) → (hard, soft).
func planLimitsFake(tenant, plan, metric string, hardLimit, softLimit int) *fakeLimitsStore {
	return &fakeLimitsStore{
		plans:  map[string]string{tenant: plan},
		limits: map[[2]string][2]int{{plan, metric}: {hardLimit, softLimit}},
	}
}

// planOnlyFake answers the tenant's plan and holds no plan_limits row, the
// fail-closed path in PlanLimiter.
func planOnlyFake(tenant, plan string) *fakeLimitsStore {
	return &fakeLimitsStore{plans: map[string]string{tenant: plan}}
}

// ---------------------------------------------------------------------------
// PlanLimiter: nil / dev-mode paths
// ---------------------------------------------------------------------------

func TestCheckLimit_NilLimiter(t *testing.T) {
	var limiter *PlanLimiter
	result, err := limiter.CheckLimit(context.Background(), "tn-test", "projects", 5)
	requireNoLimitErr(t, err)
	if !result.Allowed {
		t.Error("nil limiter should allow everything")
	}
	if result.Plan != "dev" {
		t.Errorf("nil limiter plan should be 'dev', got %q", result.Plan)
	}
}

func TestCheckLimit_NilDB(t *testing.T) {
	limiter := &PlanLimiter{}
	result, err := limiter.CheckLimit(context.Background(), "tn-test", "projects", 5)
	requireNoLimitErr(t, err)
	if !result.Allowed {
		t.Error("nil DB should allow everything")
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: enterprise plan is unlimited
// ---------------------------------------------------------------------------

func TestCheckLimit_EnterprisePlan_AlwaysAllowed(t *testing.T) {
	store := &fakeLimitsStore{
		plans: map[string]string{"tn-ent": "enterprise"},
	}

	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckLimit(context.Background(), "tn-ent", "projects", 9999)
	requireNoLimitErr(t, err)

	if !result.Allowed {
		t.Error("enterprise plan should always be allowed")
	}
	if result.Plan != "enterprise" {
		t.Errorf("Plan = %q, want enterprise", result.Plan)
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: starter plan limits
// ---------------------------------------------------------------------------

func TestCheckLimit_StarterPlan_UnderLimit_Allowed(t *testing.T) {
	store := planLimitsFake("tn-starter", "starter", "projects", 1, 0)
	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckLimit(context.Background(), "tn-starter", "projects", 0)
	requireNoLimitErr(t, err)
	if !result.Allowed {
		t.Error("starter with 0 projects should be allowed (limit=1)")
	}
	if result.Plan != "starter" {
		t.Errorf("Plan = %q, want starter", result.Plan)
	}
	if result.Limit != 1 {
		t.Errorf("Limit = %d, want 1", result.Limit)
	}
}

func TestCheckLimit_StarterPlan_AtLimit_Blocked(t *testing.T) {
	store := planLimitsFake("tn-starter", "starter", "projects", 1, 0)
	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckLimit(context.Background(), "tn-starter", "projects", 1)
	requireNoLimitErr(t, err)
	if result.Allowed {
		t.Error("starter at limit (1/1) should be blocked")
	}
	if result.Current != 1 {
		t.Errorf("Current = %d, want 1", result.Current)
	}
	if result.Limit != 1 {
		t.Errorf("Limit = %d, want 1", result.Limit)
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: pro/growth plan limits
// ---------------------------------------------------------------------------

func TestCheckLimit_ProPlan_UnderLimit_Allowed(t *testing.T) {
	store := planLimitsFake("tn-pro", "pro", "projects", 3, 0)
	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckLimit(context.Background(), "tn-pro", "projects", 2)
	requireNoLimitErr(t, err)
	if !result.Allowed {
		t.Error("pro with 2/3 projects should be allowed")
	}
}

func TestCheckLimit_ProPlan_AtLimit_Blocked(t *testing.T) {
	store := planLimitsFake("tn-pro", "pro", "projects", 3, 0)
	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckLimit(context.Background(), "tn-pro", "projects", 3)
	requireNoLimitErr(t, err)
	if result.Allowed {
		t.Error("pro at limit (3/3) should be blocked")
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: builds per month
// ---------------------------------------------------------------------------

func TestCheckLimit_BuildsPerMonth_Enforced(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-test": "starter"},
		limits: map[[2]string][2]int{{"starter", "builds_per_month"}: {100, 0}},
	}

	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckLimit(context.Background(), "tn-test", "builds_per_month", 100)
	requireNoLimitErr(t, err)

	if result.Allowed {
		t.Error("at builds_per_month limit should be blocked")
	}
	if result.Metric != "builds_per_month" {
		t.Errorf("Metric = %q, want builds_per_month", result.Metric)
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: seats limit
// ---------------------------------------------------------------------------

func TestCheckLimit_SeatsLimit_Enforced(t *testing.T) {
	store := planLimitsFake("tn-team", "team", "seats", 10, 0)
	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckLimit(context.Background(), "tn-team", "seats", 10)
	requireNoLimitErr(t, err)
	if result.Allowed {
		t.Error("at seats limit should be blocked")
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: environments limit
// ---------------------------------------------------------------------------

func TestCheckLimit_EnvironmentsLimit_Enforced(t *testing.T) {
	store := planLimitsFake("tn-test", "starter", "environments", 2, 0)
	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckLimit(context.Background(), "tn-test", "environments", 2)
	requireNoLimitErr(t, err)
	if result.Allowed {
		t.Error("at environments limit should be blocked")
	}
}

func TestCheckLimit_EnvironmentsLimit_UnderLimit(t *testing.T) {
	store := planLimitsFake("tn-test", "pro", "environments", 5, 0)
	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckLimit(context.Background(), "tn-test", "environments", 3)
	requireNoLimitErr(t, err)
	if !result.Allowed {
		t.Error("under environments limit should be allowed")
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: soft limit (overage) behavior
// ---------------------------------------------------------------------------

func TestCheckLimit_SoftLimit_OverageAllowed(t *testing.T) {
	// Hard limit 10, soft limit 8 — at 9, allowed but SoftOver=true.
	store := planLimitsFake("tn-team", "team", "seats", 10, 8)
	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckLimit(context.Background(), "tn-team", "seats", 9)
	requireNoLimitErr(t, err)
	if !result.Allowed {
		t.Error("soft over should still be allowed")
	}
	if !result.SoftOver {
		t.Error("SoftOver should be true when at/above soft limit but below hard")
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: negative hard limit (unlimited metric)
// ---------------------------------------------------------------------------

func TestCheckLimit_NegativeHardLimit_Unlimited(t *testing.T) {
	// A hard limit of -1 (the store's reading of a NULL hard_limit) is
	// unlimited.
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-test": "pro"},
		limits: map[[2]string][2]int{{"pro", "builds_per_month"}: {-1, 0}},
	}

	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckLimit(context.Background(), "tn-test", "builds_per_month", 999)
	requireNoLimitErr(t, err)
	if !result.Allowed {
		t.Error("NULL hard limit should mean unlimited")
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: missing subscription row → fallback to starter
// ---------------------------------------------------------------------------

func TestCheckLimit_MissingSubscription_FallbackStarter(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{},
		limits: map[[2]string][2]int{{"starter", "projects"}: {1, 0}},
	}

	limiter := NewPlanLimiter(store, testLogger())

	result, err := limiter.CheckLimit(context.Background(), "tn-unknown", "projects", 0)
	requireNoLimitErr(t, err)

	if result.Plan != "starter" {
		t.Errorf("Plan = %q, want starter (fallback)", result.Plan)
	}
	if !result.Allowed {
		t.Error("0/1 should be allowed on starter")
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: missing plan_limits row → deny by default
// ---------------------------------------------------------------------------

func TestCheckLimit_MissingLimitsRow_DeniedByDefault(t *testing.T) {
	store := planOnlyFake("tn-test", "starter")
	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckLimit(context.Background(), "tn-test", "unknown_metric", 0)
	requireNoLimitErr(t, err)
	if result.Allowed {
		t.Error("missing plan_limits row should deny by default (fail closed)")
	}
}

// ---------------------------------------------------------------------------
// CheckValue — B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059)
// ---------------------------------------------------------------------------
// Unlike CheckLimit (which answers "is this operation within limits given
// current usage?"), CheckValue answers "is this REQUESTED value within the
// plan cap?". Used for per-resource intent checks like replica counts.

func TestCheckValue_NilLimiter_Allowed(t *testing.T) {
	var limiter *PlanLimiter
	result, err := limiter.CheckValue(context.Background(), "tn-test", "max_replicas_per_service", 999)
	requireNoLimitErr(t, err)
	if !result.Allowed {
		t.Error("nil limiter should allow everything")
	}
}

func TestCheckValue_EnterprisePlan_AlwaysAllowed(t *testing.T) {
	store := &fakeLimitsStore{
		plans: map[string]string{"tn-ent": "enterprise"},
	}

	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckValue(context.Background(), "tn-ent", "max_replicas_per_service", 1000)
	requireNoLimitErr(t, err)

	if !result.Allowed {
		t.Error("enterprise plan should always be allowed")
	}
}

func TestCheckValue_StarterCap2_Requested2_Allowed(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-starter": "starter"},
		limits: map[[2]string][2]int{{"starter", "max_replicas_per_service"}: {2, 0}},
	}

	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckValue(context.Background(), "tn-starter", "max_replicas_per_service", 2)
	requireNoLimitErr(t, err)

	if !result.Allowed {
		t.Errorf("requested==cap should be allowed, got %+v", result)
	}
	if result.Limit != 2 {
		t.Errorf("Limit = %d, want 2", result.Limit)
	}
	if result.Current != 2 {
		t.Errorf("Current (= requested value) = %d, want 2", result.Current)
	}
}

func TestCheckValue_StarterCap2_Requested3_Blocked(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-starter": "starter"},
		limits: map[[2]string][2]int{{"starter", "max_replicas_per_service"}: {2, 0}},
	}

	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckValue(context.Background(), "tn-starter", "max_replicas_per_service", 3)
	requireNoLimitErr(t, err)

	if result.Allowed {
		t.Error("requested > cap should be blocked")
	}
	if result.Limit != 2 || result.Current != 3 {
		t.Errorf("expected Limit=2 Current=3, got %+v", result)
	}
	if result.Plan != "starter" {
		t.Errorf("Plan = %q, want starter", result.Plan)
	}
}

func TestCheckValue_NullHardLimit_Unlimited(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-team": "team"},
		limits: map[[2]string][2]int{{"team", "max_replicas_per_service"}: {-1, 0}},
	}

	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckValue(context.Background(), "tn-team", "max_replicas_per_service", 100)
	requireNoLimitErr(t, err)

	if !result.Allowed {
		t.Error("NULL hard_limit should mean unlimited")
	}
}

func TestCheckValue_MissingPlanLimitsRow_DenyByDefault(t *testing.T) {
	store := &fakeLimitsStore{
		plans: map[string]string{"tn-starter": "starter"},
	}

	limiter := NewPlanLimiter(store, testLogger())
	result, err := limiter.CheckValue(context.Background(), "tn-starter", "max_replicas_per_service", 5)
	requireNoLimitErr(t, err)

	if result.Allowed {
		t.Error("missing plan_limits row should deny by default (fail closed)")
	}
}
