// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// fakeLimitsStore is an in-memory LimitsStore for tests, demonstrating
// the DIP refactor introduced by ADR-0033. Compare this with the
// 991-LOC sqlmock setup in limiter_test.go — same coverage, no SQL
// driver involved.
type fakeLimitsStore struct {
	plans        map[string]string    // tenantID -> tenant-scoped plan
	accountPlans map[string]string    // tenantID -> account-scoped plan (M4)
	limits       map[[2]string][2]int // [plan, metric] -> [hard, soft]
	planErr      error
	limErr       error
}

func (f *fakeLimitsStore) GetTenantPlan(_ context.Context, tenantID string) (string, error) {
	if f.planErr != nil {
		return "", f.planErr
	}
	if p, ok := f.plans[tenantID]; ok {
		return p, nil
	}
	return "", sql.ErrNoRows
}

// GetAccountPlanByTenant (M4) resolves the plan of the tenant's Account.
func (f *fakeLimitsStore) GetAccountPlanByTenant(_ context.Context, tenantID string) (string, error) {
	if f.planErr != nil {
		return "", f.planErr
	}
	if p, ok := f.accountPlans[tenantID]; ok {
		return p, nil
	}
	return "", sql.ErrNoRows
}

// M4 T1.2 — CheckProjectsValue resolves the ACCOUNT plan (not the tenant plan)
// and the projects cap; allowed iff creating one more stays within the cap
// (current < hard), with enterprise/unlimited (hard<0) always allowed.
func TestCheckProjectsValue_AccountScoped_StarterCap1(t *testing.T) {
	store := &fakeLimitsStore{
		accountPlans: map[string]string{"tn-a": "starter", "tn-b": "starter"}, // both tenants → same account plan
		limits:       map[[2]string][2]int{{"starter", "projects"}: {1, 0}},
	}
	limiter := NewPlanLimiterWithStore(store, testLogger())

	if r := limiter.CheckProjectsValue("tn-a", 0); !r.Allowed || r.Plan != "starter" || r.Limit != 1 {
		t.Fatalf("under cap must allow; got %+v", r)
	}
	if r := limiter.CheckProjectsValue("tn-a", 1); r.Allowed {
		t.Fatalf("at cap (current=1, cap=1) must deny; got %+v", r)
	}
	if r := limiter.CheckProjectsValue("tn-b", 1); r.Allowed {
		t.Fatalf("2nd tenant of the same account must also be denied at cap; got %+v", r)
	}
}

func TestCheckProjectsValue_Enterprise_Unlimited(t *testing.T) {
	store := &fakeLimitsStore{
		accountPlans: map[string]string{"tn-c": "enterprise"},
		limits:       map[[2]string][2]int{{"enterprise", "projects"}: {-1, 0}},
	}
	limiter := NewPlanLimiterWithStore(store, testLogger())
	r := limiter.CheckProjectsValue("tn-c", 999999)
	if !r.Allowed {
		t.Fatalf("enterprise (unlimited) must always allow; got %+v", r)
	}
	if r.Limit != -1 {
		t.Fatalf("unlimited must report Limit=-1 (the skip-lock sentinel, EC-6); got %+v", r)
	}
}

func (f *fakeLimitsStore) GetLimits(_ context.Context, plan, metric string) (int, int, error) {
	if f.limErr != nil {
		return 0, 0, f.limErr
	}
	v, ok := f.limits[[2]string{plan, metric}]
	if !ok {
		return 0, 0, sql.ErrNoRows
	}
	return v[0], v[1], nil
}

// ---------------------------------------------------------------------------
// T2.4 — DIP test cases per ADR-0033.
// ---------------------------------------------------------------------------

func TestNewPlanLimiterWithStore_NilStoreFallsThroughDevMode(t *testing.T) {
	limiter := NewPlanLimiterWithStore(nil, testLogger())
	got := limiter.CheckLimit("tn-test", "projects", 5)
	if !got.Allowed || got.Plan != "dev" {
		t.Errorf("nil store should fall through to dev mode; got %+v", got)
	}
}

func TestCheckLimit_WithFakeStore_AllowsWhenUnderHardLimit(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-acme": "starter"},
		limits: map[[2]string][2]int{{"starter", "projects"}: {3, 0}},
	}
	limiter := NewPlanLimiterWithStore(store, testLogger())
	got := limiter.CheckLimit("tn-acme", "projects", 2)
	if !got.Allowed {
		t.Errorf("under-hard-limit must allow; got %+v", got)
	}
	if got.Limit != 3 || got.Current != 2 {
		t.Errorf("unexpected limit/current: %+v", got)
	}
}

func TestCheckLimit_WithFakeStore_DeniesAtHardLimit(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-acme": "starter"},
		limits: map[[2]string][2]int{{"starter", "projects"}: {3, 0}},
	}
	limiter := NewPlanLimiterWithStore(store, testLogger())
	got := limiter.CheckLimit("tn-acme", "projects", 3)
	if got.Allowed {
		t.Errorf("at-hard-limit must deny; got %+v", got)
	}
}

func TestCheckLimit_WithFakeStore_NullHardLimitMeansUnlimited(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-acme": "team"},
		limits: map[[2]string][2]int{{"team", "projects"}: {-1, 0}}, // hard < 0 = unlimited
	}
	limiter := NewPlanLimiterWithStore(store, testLogger())
	got := limiter.CheckLimit("tn-acme", "projects", 9999)
	if !got.Allowed {
		t.Errorf("hard==-1 (NULL) must mean unlimited; got %+v", got)
	}
}

func TestCheckLimit_WithFakeStore_MissingPlanFallsBackToStarter(t *testing.T) {
	called := false
	store := &fakeLimitsStore{
		plans:  map[string]string{}, // tenant has no subscription
		limits: map[[2]string][2]int{{"starter", "projects"}: {3, 0}},
	}
	limiter := NewPlanLimiterWithStore(store, testLogger())
	limiter.OnFallbackStarter = func() { called = true }

	got := limiter.CheckLimit("tn-orphan", "projects", 4)
	if !called {
		t.Error("OnFallbackStarter must fire when plan missing")
	}
	if got.Plan != "starter" {
		t.Errorf("missing subscription must fall back to starter; got %q", got.Plan)
	}
	if got.Allowed {
		t.Errorf("starter limit (3) must deny request for 4; got %+v", got)
	}
}

func TestCheckLimit_WithFakeStore_MissingLimitRowFailsClosed(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-acme": "pro"},
		limits: map[[2]string][2]int{}, // no rows for any (plan,metric)
	}
	limiter := NewPlanLimiterWithStore(store, testLogger())
	got := limiter.CheckLimit("tn-acme", "secrets_per_env", 1)
	if got.Allowed {
		t.Error("missing limit row must fail closed")
	}
}

func TestCheckLimit_WithFakeStore_EnterpriseAlwaysAllows(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-corp": "enterprise"},
		limits: map[[2]string][2]int{},
	}
	limiter := NewPlanLimiterWithStore(store, testLogger())
	got := limiter.CheckLimit("tn-corp", "projects", 1000000)
	if !got.Allowed {
		t.Errorf("enterprise plan must always allow; got %+v", got)
	}
}

func TestCheckValue_WithFakeStore_DeniesWhenRequestedExceedsCap(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-acme": "starter"},
		limits: map[[2]string][2]int{{"starter", "replicas"}: {2, 0}},
	}
	limiter := NewPlanLimiterWithStore(store, testLogger())
	got := limiter.CheckValue("tn-acme", "replicas", 5)
	if got.Allowed {
		t.Error("requested 5 over hard 2 must deny")
	}
	if got.Current != 5 || got.Limit != 2 {
		t.Errorf("CheckValue must report requested as Current and cap as Limit; got %+v", got)
	}
}

func TestStore_PropagatesGenericError(t *testing.T) {
	bug := errors.New("driver crashed")
	store := &fakeLimitsStore{planErr: bug}
	limiter := NewPlanLimiterWithStore(store, testLogger())
	got := limiter.CheckLimit("tn-x", "projects", 0)
	// On unknown error the limiter falls back to starter (existing
	// behavior preserved); the test pins the contract.
	if got.Plan != "starter" {
		t.Errorf("unknown store error should fall back to starter; got %+v", got)
	}
}
