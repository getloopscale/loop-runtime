// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package enforcement

import (
	"context"
	"errors"
	"testing"
)

// fakeLimitsStore is an in-memory LimitsStore for tests (ADR-0033). The SQL
// adapter and its tests live in the api module, next to the schema.
type fakeLimitsStore struct {
	plans        map[string]string    // tenantID -> tenant-scoped plan
	accountPlans map[string]string    // tenantID -> account-scoped plan (M4)
	limits       map[[2]string][2]int // [plan, metric] -> [hard, soft]
	planErr      error
	limErr       error
}

func (f *fakeLimitsStore) GetTenantPlan(ctx context.Context, tenantID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if f.planErr != nil {
		return "", f.planErr
	}
	return f.plans[tenantID], nil // "" = no subscription (LimitsStore contract)
}

// GetAccountPlanByTenant (M4) resolves the plan of the tenant's Account.
func (f *fakeLimitsStore) GetAccountPlanByTenant(_ context.Context, tenantID string) (string, error) {
	if f.planErr != nil {
		return "", f.planErr
	}
	return f.accountPlans[tenantID], nil // "" = no subscription
}

// M4 T1.2 — CheckProjectsValue resolves the ACCOUNT plan (not the tenant plan)
// and the projects cap; allowed iff creating one more stays within the cap
// (current < hard), with enterprise/unlimited (hard<0) always allowed.
func TestCheckProjectsValue_AccountScoped_StarterCap1(t *testing.T) {
	store := &fakeLimitsStore{
		accountPlans: map[string]string{"tn-a": "starter", "tn-b": "starter"}, // both tenants → same account plan
		limits:       map[[2]string][2]int{{"starter", "projects"}: {1, 0}},
	}
	limiter := NewPlanLimiter(store, testLogger())

	r, err := limiter.CheckProjectsValue(context.Background(), "tn-a", 0)
	requireNoLimitErr(t, err)
	if !r.Allowed || r.Plan != "starter" || r.Limit != 1 {
		t.Fatalf("under cap must allow; got %+v", r)
	}
	r, err = limiter.CheckProjectsValue(context.Background(), "tn-a", 1)
	requireNoLimitErr(t, err)
	if r.Allowed {
		t.Fatalf("at cap (current=1, cap=1) must deny; got %+v", r)
	}
	r, err = limiter.CheckProjectsValue(context.Background(), "tn-b", 1)
	requireNoLimitErr(t, err)
	if r.Allowed {
		t.Fatalf("2nd tenant of the same account must also be denied at cap; got %+v", r)
	}
}

func TestCheckProjectsValue_Enterprise_Unlimited(t *testing.T) {
	store := &fakeLimitsStore{
		accountPlans: map[string]string{"tn-c": "enterprise"},
		limits:       map[[2]string][2]int{{"enterprise", "projects"}: {-1, 0}},
	}
	limiter := NewPlanLimiter(store, testLogger())
	r, err := limiter.CheckProjectsValue(context.Background(), "tn-c", 999999)
	requireNoLimitErr(t, err)
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
		return 0, 0, nil // missing row: the (0, 0) deny sentinel (LimitsStore contract)
	}
	return v[0], v[1], nil
}

// ---------------------------------------------------------------------------
// T2.4 — DIP test cases per ADR-0033.
// ---------------------------------------------------------------------------

func TestNewPlanLimiter_NilStoreFallsThroughDevMode(t *testing.T) {
	limiter := NewPlanLimiter(nil, testLogger())
	got, err := limiter.CheckLimit(context.Background(), "tn-test", "projects", 5)
	requireNoLimitErr(t, err)
	if !got.Allowed || got.Plan != "dev" {
		t.Errorf("nil store should fall through to dev mode; got %+v", got)
	}
}

func TestCheckLimit_WithFakeStore_AllowsWhenUnderHardLimit(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-acme": "starter"},
		limits: map[[2]string][2]int{{"starter", "projects"}: {3, 0}},
	}
	limiter := NewPlanLimiter(store, testLogger())
	got, err := limiter.CheckLimit(context.Background(), "tn-acme", "projects", 2)
	requireNoLimitErr(t, err)
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
	limiter := NewPlanLimiter(store, testLogger())
	got, err := limiter.CheckLimit(context.Background(), "tn-acme", "projects", 3)
	requireNoLimitErr(t, err)
	if got.Allowed {
		t.Errorf("at-hard-limit must deny; got %+v", got)
	}
}

func TestCheckLimit_WithFakeStore_NullHardLimitMeansUnlimited(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-acme": "team"},
		limits: map[[2]string][2]int{{"team", "projects"}: {-1, 0}}, // hard < 0 = unlimited
	}
	limiter := NewPlanLimiter(store, testLogger())
	got, err := limiter.CheckLimit(context.Background(), "tn-acme", "projects", 9999)
	requireNoLimitErr(t, err)
	if !got.Allowed {
		t.Errorf("hard==-1 (NULL) must mean unlimited; got %+v", got)
	}
}

func TestCheckLimit_WithFakeStore_MissingPlanFallsBackToStarter(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{}, // tenant has no subscription
		limits: map[[2]string][2]int{{"starter", "projects"}: {3, 0}},
	}
	limiter := NewPlanLimiter(store, testLogger())

	got, err := limiter.CheckLimit(context.Background(), "tn-orphan", "projects", 4)
	requireNoLimitErr(t, err)
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
	limiter := NewPlanLimiter(store, testLogger())
	got, err := limiter.CheckLimit(context.Background(), "tn-acme", "secrets_per_env", 1)
	requireNoLimitErr(t, err)
	if got.Allowed {
		t.Error("missing limit row must fail closed")
	}
}

func TestCheckLimit_WithFakeStore_EnterpriseAlwaysAllows(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-corp": "enterprise"},
		limits: map[[2]string][2]int{},
	}
	limiter := NewPlanLimiter(store, testLogger())
	got, err := limiter.CheckLimit(context.Background(), "tn-corp", "projects", 1000000)
	requireNoLimitErr(t, err)
	if !got.Allowed {
		t.Errorf("enterprise plan must always allow; got %+v", got)
	}
}

func TestCheckValue_WithFakeStore_DeniesWhenRequestedExceedsCap(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-acme": "starter"},
		limits: map[[2]string][2]int{{"starter", "replicas"}: {2, 0}},
	}
	limiter := NewPlanLimiter(store, testLogger())
	got, err := limiter.CheckValue(context.Background(), "tn-acme", "replicas", 5)
	requireNoLimitErr(t, err)
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
	limiter := NewPlanLimiter(store, testLogger())

	_, err := limiter.CheckLimit(context.Background(), "tn-x", "projects", 0)

	requireDegraded(t, err, bug)
}
