// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

// project_cap_gate_test.go — M5 (hermetic project-cap, #106) unit coverage for
// the per-Account cap gate shared by createProjectCapped (API) and EnsureProject
// (deploy). The atomic advisory-lock behavior + the full EnsureProject deploy
// path are proven against real Postgres in
// api/tests/integration/project_cap_deploy_path_test.go (sqlmock cannot prove
// pg_advisory_xact_lock semantics — same rationale as M4 XVAL-3). These unit
// tests assert the decision logic + the fail-closed branch.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db/dbtest"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// --- test doubles ---

// capValRow scans preset values (int/string) into the destinations in order.
type capValRow struct{ vals []any }

func (r capValRow) Scan(dest ...any) error {
	for i := range dest {
		if i >= len(r.vals) {
			break
		}
		switch d := dest[i].(type) {
		case *int:
			*d, _ = r.vals[i].(int)
		case *string:
			*d, _ = r.vals[i].(string)
		}
	}
	return nil
}

// capErrRow returns a fixed error from Scan.
type capErrRow struct{ err error }

func (r capErrRow) Scan(_ ...any) error { return r.err }

// fakeCapChecker is a fake ProjectCapChecker.
type fakeCapChecker struct {
	allowed bool
	plan    string
	cap     int
	err     error
}

func (f fakeCapChecker) CheckProjectsValue(_ context.Context, _ string, _ int) (bool, string, int, error) {
	return f.allowed, f.plan, f.cap, f.err
}

// --- enforceProjectCapTx (the cap rule, shared by both paths) ---

func TestEnforceProjectCapTx_BlocksWhenAtCap(t *testing.T) {
	// Arrange: the account already has 1 project; cap is 1 → one more would exceed.
	fake := &dbtest.FakeDBTX{QueryRowResult: capValRow{vals: []any{1}}} // count(*) = 1
	before := testutil.ToFloat64(metrics.PlanLimitViolations.WithLabelValues("max_projects_per_account", "starter"))

	// Act
	err := enforceProjectCapTx(context.Background(), fake, nil, "acct-1", "starter", 1)

	// Assert: typed PROJECTS_EXCEED_PLAN + metric incremented by exactly 1.
	var te *loopruntimeerrors.LoopRuntimeError
	if !errors.As(err, &te) || te.Code != "PROJECTS_EXCEED_PLAN" {
		t.Fatalf("expected PROJECTS_EXCEED_PLAN, got %v", err)
	}
	after := testutil.ToFloat64(metrics.PlanLimitViolations.WithLabelValues("max_projects_per_account", "starter"))
	if after-before != 1 {
		t.Fatalf("metric delta = %v, want 1", after-before)
	}
}

func TestEnforceProjectCapTx_AllowsUnderCap(t *testing.T) {
	fake := &dbtest.FakeDBTX{QueryRowResult: capValRow{vals: []any{0}}} // count(*) = 0
	before := testutil.ToFloat64(metrics.PlanLimitViolations.WithLabelValues("max_projects_per_account", "pro"))

	err := enforceProjectCapTx(context.Background(), fake, nil, "acct-2", "pro", 3)

	if err != nil {
		t.Fatalf("under cap should be allowed, got %v", err)
	}
	after := testutil.ToFloat64(metrics.PlanLimitViolations.WithLabelValues("max_projects_per_account", "pro"))
	if after != before {
		t.Fatalf("metric must stay flat under cap; delta = %v", after-before)
	}
}

func TestEnforceProjectCapTx_CapZeroFailsClosed(t *testing.T) {
	// cap == 0 (misconfigured) → count(0) >= 0 → reject (never insert unbounded).
	fake := &dbtest.FakeDBTX{QueryRowResult: capValRow{vals: []any{0}}}

	err := enforceProjectCapTx(context.Background(), fake, nil, "acct-3", "starter", 0)

	var te *loopruntimeerrors.LoopRuntimeError
	if !errors.As(err, &te) || te.Code != "PROJECTS_EXCEED_PLAN" {
		t.Fatalf("cap=0 must fail closed with PROJECTS_EXCEED_PLAN, got %v", err)
	}
}

func TestEnforceProjectCapTx_CountErrorPropagates(t *testing.T) {
	// A DB error on the count must propagate (never silently allow).
	fake := &dbtest.FakeDBTX{QueryRowResult: capErrRow{err: errors.New("boom")}}

	err := enforceProjectCapTx(context.Background(), fake, nil, "acct-4", "starter", 1)

	if err == nil {
		t.Fatal("count error must propagate, got nil")
	}
}

// --- resolveAccountAndLock (resolve + advisory lock, shared) ---

func TestResolveAccountAndLock_TakesAdvisoryLockForAccount(t *testing.T) {
	fake := &dbtest.FakeDBTX{QueryRowResult: capValRow{vals: []any{"acct-9"}}} // account_id

	accountID, err := resolveAccountAndLock(context.Background(), fake, "tenant-9")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if accountID != "acct-9" {
		t.Fatalf("accountID = %q, want acct-9", accountID)
	}
	if fake.ExecCalls != 1 {
		t.Fatalf("advisory lock Exec calls = %d, want 1", fake.ExecCalls)
	}
	if got := fake.CapturedSQL; got == "" || !strings.Contains(got, "pg_advisory_xact_lock") {
		t.Fatalf("expected advisory-lock SQL, captured %q", got)
	}
}

func TestResolveAccountAndLock_AccountResolveErrorPropagates(t *testing.T) {
	fake := &dbtest.FakeDBTX{QueryRowResult: capErrRow{err: errors.New("no tenant")}}

	_, err := resolveAccountAndLock(context.Background(), fake, "tenant-x")

	if err == nil {
		t.Fatal("account-resolve error must propagate")
	}
}

// --- EnsureProject fail-closed (EC-1: bounded cap needed, no pgx pool) ---

func TestEnsureProject_FailsClosedWhenBoundedCapNeedsPgxPoolButNoneAvailable(t *testing.T) {
	// capChecker present + bounded plan, tx == nil, and s.DB is NOT a pgx pool →
	// atomic enforcement is impossible → fail closed (error), never insert.
	store := &PostgresProjectStore{DB: &dbtest.FakeDBTX{}}
	store.SetProjectCapChecker(fakeCapChecker{allowed: false, plan: "starter", cap: 1})

	err := store.EnsureProject(context.Background(), nil, "tenant-1", "newproj", "user-1")

	if err == nil {
		t.Fatal("bounded cap without a pgx pool must fail closed, got nil")
	}
}

// A cap that cannot be verified refuses the insert with the checker's error.
func TestEnsureProject_CapCheckErrorPropagates(t *testing.T) {
	boom := errors.New("limits store down")
	store := &PostgresProjectStore{DB: &dbtest.FakeDBTX{}}
	store.SetProjectCapChecker(fakeCapChecker{err: boom})

	err := store.EnsureProject(context.Background(), nil, "tenant-1", "newproj", "user-1")

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the cap-check error", err)
	}
}

func TestCreateProject_CapCheckErrorPropagates(t *testing.T) {
	boom := errors.New("limits store down")
	store := &PostgresProjectStore{DB: &dbtest.FakeDBTX{}}
	store.SetProjectCapChecker(fakeCapChecker{err: boom})

	_, err := store.CreateProject(context.Background(), ports.CreateProjectInput{TenantID: "tenant-1", Name: "newproj"})

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the cap-check error", err)
	}
}
