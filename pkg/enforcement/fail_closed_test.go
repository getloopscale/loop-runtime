// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"context"
	"errors"
	"testing"

	"github.com/getloopscale/loopzero/pkg/domain"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

var errStoreDown = errors.New("connection refused")

func requireNoLimitErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected limit-check error: %v", err)
	}
}

// requireDegraded asserts err is the PLATFORM_DEGRADED LoopZeroError and still
// carries its cause.
func requireDegraded(t *testing.T, err, cause error) {
	t.Helper()
	if !errors.Is(err, loopzeroerrors.ErrPlatformDegraded) {
		t.Fatalf("err = %v, want PLATFORM_DEGRADED", err)
	}
	if cause != nil && !errors.Is(err, cause) {
		t.Fatalf("err = %v, want it to wrap %v", err, cause)
	}
}

// A store outage must never read as "the tenant is on starter". Starter has
// room for new work, so the old fallback turned a database outage into an
// allow decision for every tenant whose usage happened to be low.
func TestCheckLimit_FailsClosedWhenThePlanCannotBeResolved(t *testing.T) {
	store := &fakeLimitsStore{
		planErr: errStoreDown,
		limits:  map[[2]string][2]int{{"starter", "projects"}: {3, 0}},
	}
	limiter := NewPlanLimiter(store, testLogger())

	_, err := limiter.CheckLimit(context.Background(), "tn-x", "projects", 0)

	requireDegraded(t, err, errStoreDown)
}

// A limits-table outage is an infrastructure fault, not "no limit configured".
func TestCheckLimit_FailsClosedWhenTheLimitsCannotBeRead(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-x": "team"},
		limErr: errStoreDown,
	}
	limiter := NewPlanLimiter(store, testLogger())

	_, err := limiter.CheckLimit(context.Background(), "tn-x", "projects", 0)

	requireDegraded(t, err, errStoreDown)
}

// A missing row stays the deny sentinel, not an outage.
func TestCheckLimit_MissingLimitRowDeniesWithoutError(t *testing.T) {
	store := &fakeLimitsStore{plans: map[string]string{"tn-x": "pro"}}
	limiter := NewPlanLimiter(store, testLogger())

	got, err := limiter.CheckLimit(context.Background(), "tn-x", "secrets_per_env", 0)

	requireNoLimitErr(t, err)
	if got.Allowed || got.Limit != 0 {
		t.Fatalf("missing plan_limits row must deny with Limit 0, got %+v", got)
	}
}

// ADR D4: a legacy "growth" subscription is a Pro tenant. The limiter used to
// look up plan_limits for "growth", find no row and deny everything.
func TestCheckLimit_TreatsLegacyGrowthPlanAsPro(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-old": "growth"},
		limits: map[[2]string][2]int{{"pro", "projects"}: {10, 0}},
	}
	limiter := NewPlanLimiter(store, testLogger())

	got, err := limiter.CheckLimit(context.Background(), "tn-old", "projects", 2)

	requireNoLimitErr(t, err)
	if !got.Allowed || got.Plan != "pro" || got.Limit != 10 {
		t.Fatalf("growth must resolve to pro's limits, got %+v", got)
	}
}

// ADR D4: an unrecognised plan value is never silently downgraded to starter.
func TestCheckLimit_RefusesAnUnrecognisedPlanValue(t *testing.T) {
	store := &fakeLimitsStore{
		plans:  map[string]string{"tn-x": "platinum"},
		limits: map[[2]string][2]int{{"starter", "projects"}: {3, 0}},
	}
	limiter := NewPlanLimiter(store, testLogger())

	_, err := limiter.CheckLimit(context.Background(), "tn-x", "projects", 0)

	requireDegraded(t, err, domain.ErrUnknownPlanTier)
}

// The request context reaches the store, so cancellation and deadlines apply
// to the plan and limit queries.
func TestCheckLimit_PassesTheCallerContextToTheStore(t *testing.T) {
	store := &fakeLimitsStore{plans: map[string]string{"tn-x": "pro"}}
	limiter := NewPlanLimiter(store, testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := limiter.CheckLimit(ctx, "tn-x", "projects", 0)

	requireDegraded(t, err, context.Canceled)
}

// M4 EC-2: an Account with no active subscription is capped as starter. The
// store answers "" for it (LimitsStore contract); a fail-closed limiter must
// not turn that into an outage for every new Account.
func TestCheckProjectsValue_AccountWithoutSubscriptionIsStarter(t *testing.T) {
	store := &fakeLimitsStore{
		accountPlans: map[string]string{},
		limits:       map[[2]string][2]int{{"starter", "projects"}: {1, 0}},
	}
	limiter := NewPlanLimiter(store, testLogger())

	got, err := limiter.CheckProjectsValue(context.Background(), "tn-new", 0)

	requireNoLimitErr(t, err)
	if !got.Allowed || got.Plan != "starter" || got.Limit != 1 {
		t.Fatalf("new Account must get the starter cap, got %+v", got)
	}
}

// A usage counter that cannot count must stop the operation. Reading the
// failure as "zero used" lets every quota pass during a database outage.
func TestCheckUsage_FailsClosedWhenUsageCannotBeCounted(t *testing.T) {
	limiter := NewPlanLimiter(&fakeLimitsStore{
		plans:  map[string]string{"tn-x": "team"},
		limits: map[[2]string][2]int{{"team", "seats"}: {10, 0}},
	}, testLogger())

	_, err := limiter.CheckUsage(context.Background(), "tn-x", "seats",
		func(context.Context, string) (int, error) { return 0, errStoreDown })

	requireDegraded(t, err, errStoreDown)
}

func TestCheckUsage_ChecksTheCountedUsageAgainstTheLimit(t *testing.T) {
	limiter := NewPlanLimiter(&fakeLimitsStore{
		plans:  map[string]string{"tn-x": "team"},
		limits: map[[2]string][2]int{{"team", "seats"}: {10, 0}},
	}, testLogger())

	got, err := limiter.CheckUsage(context.Background(), "tn-x", "seats",
		func(context.Context, string) (int, error) { return 10, nil })

	requireNoLimitErr(t, err)
	if got.Allowed || got.Current != 10 || got.Limit != 10 {
		t.Fatalf("10 of 10 seats must deny, got %+v", got)
	}
}

func TestCheckUsage_DevModeDoesNotCountUsage(t *testing.T) {
	counted := false

	got, err := NewPlanLimiter(nil, testLogger()).CheckUsage(context.Background(), "tn-x", "seats",
		func(context.Context, string) (int, error) { counted = true; return 0, nil })

	requireNoLimitErr(t, err)
	if counted || !got.Allowed {
		t.Fatalf("dev mode must allow without counting; counted=%v got=%+v", counted, got)
	}
}
