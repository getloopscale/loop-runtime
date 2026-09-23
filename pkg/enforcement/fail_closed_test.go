// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

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
// SQL store used to return sql.ErrNoRows for it, which a fail-closed limiter
// would turn into an outage for every new Account.
func TestCheckProjectsValue_AccountWithoutSubscriptionIsStarter(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("SELECT plan FROM subscriptions").WithArgs("tn-new").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").WithArgs("starter", "projects").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(1, nil))
	limiter := NewPlanLimiter(NewSQLLimitsStore(db), testLogger())

	got, err := limiter.CheckProjectsValue(context.Background(), "tn-new", 0)

	requireNoLimitErr(t, err)
	if !got.Allowed || got.Plan != "starter" || got.Limit != 1 {
		t.Fatalf("new Account must get the starter cap, got %+v", got)
	}
}

func serveThroughMiddleware(t *testing.T, limiter *PlanLimiter, count UsageCounter) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	innerCalled := false
	mw := Middleware(limiter, OperationCheck{Metric: "seats", CountFn: count},
		func(*http.Request) string { return "tn-x" }, "/billing")
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { innerCalled = true })).
		ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/members", nil))
	return rec, innerCalled
}

// A usage counter that cannot count must stop the request. Reading the failure
// as "zero used" lets every quota pass during a database outage.
func TestMiddleware_Answers503WhenUsageCannotBeCounted(t *testing.T) {
	limiter := NewPlanLimiter(&fakeLimitsStore{
		plans:  map[string]string{"tn-x": "team"},
		limits: map[[2]string][2]int{{"team", "seats"}: {10, 0}},
	}, testLogger())

	rec, innerCalled := serveThroughMiddleware(t, limiter,
		func(context.Context, string) (int, error) { return 0, errStoreDown })

	if innerCalled || rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("count failure must answer 503 without calling the handler; got %d, inner=%v", rec.Code, innerCalled)
	}
}

func TestMiddleware_Answers503WhenThePlanCannotBeResolved(t *testing.T) {
	limiter := NewPlanLimiter(&fakeLimitsStore{planErr: errStoreDown}, testLogger())

	rec, innerCalled := serveThroughMiddleware(t, limiter,
		func(context.Context, string) (int, error) { return 0, nil })

	if innerCalled || rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("plan failure must answer 503 without calling the handler; got %d, inner=%v", rec.Code, innerCalled)
	}
}

func TestMiddleware_DevModeDoesNotCountUsage(t *testing.T) {
	counted := false

	rec, innerCalled := serveThroughMiddleware(t, NewPlanLimiter(nil, testLogger()),
		func(context.Context, string) (int, error) { counted = true; return 0, nil })

	if counted || !innerCalled || rec.Code != http.StatusOK {
		t.Fatalf("dev mode must pass through without counting; counted=%v inner=%v code=%d", counted, innerCalled, rec.Code)
	}
}
