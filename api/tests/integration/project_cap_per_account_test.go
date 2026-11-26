// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package integration

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/dbtx"
	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/pkg/enforcement"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// violationCount reads the per-plan project-cap violation counter (M4 metric).
func violationCount(plan string) float64 {
	return testutil.ToFloat64(metrics.PlanLimitViolations.WithLabelValues("max_projects_per_account", plan))
}

// M4 T3.2 — the per-Account project cap, proven against real PostgreSQL.
// Proves the cap is per-ACCOUNT (binds across all the Account's tenants), uses
// the canonical 422 PROJECTS_EXCEED_PLAN typed error, leaves enterprise
// unlimited, blocks-new-only for grandfathered accounts, and never exceeds the
// cap under concurrency (the advisory-lock atomic guard).
//
//	DATABASE_URL=postgres://loopruntime:loopruntime@host/db?sslmode=disable go test ./tests/integration/ -run ProjectCapPerAccount -race -v

// realProjectCapChecker adapts a real PlanLimiter to store.ProjectCapChecker.
type realProjectCapChecker struct{ l *enforcement.PlanLimiter }

func (c realProjectCapChecker) CheckProjectsValue(ctx context.Context, tenantID string, current int) (bool, string, int, error) {
	r, err := c.l.CheckProjectsValue(ctx, tenantID, current)
	if err != nil {
		return false, "", 0, err
	}
	return r.Allowed, r.Plan, r.Limit, nil
}

func projectCapStore(t *testing.T) (*store.PostgresProjectStore, *sql.DB) {
	t.Helper()
	_ = testPool(t) // skips if pg unreachable
	stdDB, err := sql.Open("pgx", seedDBURL())
	if err != nil {
		t.Fatalf("open stdlib DB: %v", err)
	}
	t.Cleanup(func() { _ = stdDB.Close() })
	// The plan is read through the cross-tenant worker role, as main wires it
	// (wirePlanEnforcement): seedDBURL is LOOPRUNTIME_WORKER_DATABASE_URL.
	limiter := enforcement.NewPlanLimiter(store.NewPlanLimitsStore(&db.PgxAdapter{Pool: workerPool(t)}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	// The store runs as main wires it: on the request pool, whose connections
	// carry the caller's tenant scope, and counting the Account's projects
	// through the cross-tenant pool (wireProjectCap).
	store := &store.PostgresProjectStore{DB: scopedRequestPool(t)}
	store.SetProjectCapChecker(realProjectCapChecker{l: limiter})
	store.SetCrossTenantCounter(&db.PgxAdapter{Pool: workerPool(t)})
	return store, stdDB
}

// asTenant is the context an authenticated request of tenantID reaches the
// store with.
func asTenant(tenantID string) context.Context {
	return dbtx.WithTenantScope(context.Background(), tenantID)
}

func projectCapCleanup(t *testing.T, std *sql.DB, owner string) {
	t.Helper()
	for _, s := range []string{
		`DELETE FROM projects WHERE tenant_id IN (SELECT id FROM tenants WHERE owner_id=$1)`,
		`DELETE FROM subscriptions WHERE account_id IN (SELECT account_id FROM tenants WHERE owner_id=$1)`,
		`DELETE FROM tenant_members WHERE user_id=$1`,
		`DELETE FROM tenants WHERE owner_id=$1`,
	} {
		if _, err := std.Exec(s, owner); err != nil {
			t.Fatalf("cleanup %q: %v", s, err)
		}
	}
	if err := execAsAccountsOwner(context.Background(), `DELETE FROM accounts WHERE owner_id=$1`, owner); err != nil {
		t.Fatalf("cleanup accounts: %v", err)
	}
	if _, err := std.Exec(`DELETE FROM users WHERE id=$1`, owner); err != nil {
		t.Fatalf("cleanup users: %v", err)
	}
}

// projectCapSeed seeds an Account (with `plan`) owning two tenants + one account-scoped
// subscription. Returns nothing; ids are derived from owner.
func projectCapSeed(t *testing.T, std *sql.DB, owner, acct, tn1, tn2, plan string) {
	t.Helper()
	projectCapCleanup(t, std, owner)
	exec := func(q string, a ...any) {
		if _, err := std.Exec(q, a...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id,email,display_name,username) VALUES ($1,$1||'@e2e.test','O',$1)`, owner)
	if err := execAsAccountsOwner(context.Background(),
		`INSERT INTO accounts (id,display_name,plan,owner_id,created_at,updated_at) VALUES ($1,'A',$2,$3,now(),now())`,
		acct, plan, owner); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	exec(`INSERT INTO tenants (id,display_name,slug,owner_id,account_id,cell_id) VALUES ($1,'T1',$1,$2,$3,'cell-0')`, tn1, owner, acct)
	exec(`INSERT INTO tenants (id,display_name,slug,owner_id,account_id,cell_id) VALUES ($1,'T2',$1,$2,$3,'cell-0')`, tn2, owner, acct)
	exec(`INSERT INTO subscriptions (id,tenant_id,account_id,plan,status) VALUES ($1,$2,$3,$4,'active') ON CONFLICT (account_id) DO NOTHING`, "sub-"+acct, tn1, acct, plan)
}

func mustCreate(t *testing.T, store *store.PostgresProjectStore, tenant, name, owner string) error {
	t.Helper()
	_, err := store.CreateProject(asTenant(tenant), ports.CreateProjectInput{TenantID: tenant, Name: name, CreatedBy: owner})
	return err
}

func isCapError(err error) bool {
	var te *loopruntimeerrors.LoopRuntimeError
	return errors.As(err, &te) && te.Code == "PROJECTS_EXCEED_PLAN"
}

// Scenario 1 — the cap is per-ACCOUNT: a Starter account (cap=1) is blocked at
// the 2nd project EVEN when the 2nd is created under a DIFFERENT tenant of the
// same account.
func TestProjectCapPerAccount_PerAccount_BlocksAcrossTenants(t *testing.T) {
	store, std := projectCapStore(t)
	const owner, acct, tn1, tn2 = "m4-uid", "acct-m4", "tn-m4-a", "tn-m4-b"
	projectCapSeed(t, std, owner, acct, tn1, tn2, "starter") // starter cap = 1
	t.Cleanup(func() { projectCapCleanup(t, std, owner) })

	if err := mustCreate(t, store, tn1, "first", owner); err != nil {
		t.Fatalf("1st project (under cap) must succeed: %v", err)
	}
	// 2nd project under the OTHER tenant of the same account → over the Account cap.
	before := violationCount("starter")
	err := mustCreate(t, store, tn2, "second", owner)
	var te *loopruntimeerrors.LoopRuntimeError
	if !errors.As(err, &te) || te.Code != "PROJECTS_EXCEED_PLAN" {
		t.Fatalf("2nd project (over Account cap, different tenant) must be PROJECTS_EXCEED_PLAN; got %v", err)
	}
	// DoD #5: the violation metric increments on rejection (wiring triad pillar c).
	if got := violationCount("starter"); got != before+1 {
		t.Fatalf("plan_limit_violation{metric=max_projects_per_account,plan=starter} = %v, want %v (before+1)", got, before+1)
	}
}

// Scenario 2 — enterprise (cap=NULL) is never blocked.
func TestProjectCapPerAccount_Enterprise_Unlimited(t *testing.T) {
	store, std := projectCapStore(t)
	const owner, acct, tn1, tn2 = "m4-ent-uid", "acct-m4ent", "tn-m4ent-a", "tn-m4ent-b"
	projectCapSeed(t, std, owner, acct, tn1, tn2, "enterprise")
	t.Cleanup(func() { projectCapCleanup(t, std, owner) })

	before := violationCount("enterprise")
	for i := 0; i < 5; i++ {
		if err := mustCreate(t, store, tn1, "ent-"+string(rune('a'+i)), owner); err != nil {
			t.Fatalf("enterprise must never block (project %d): %v", i, err)
		}
	}
	// EC-6: an unlimited account is never even evaluated as a violation — the
	// metric stays flat (the cap<0 path skips the lock+count+rejection entirely).
	if got := violationCount("enterprise"); got != before {
		t.Fatalf("enterprise must NOT increment the violation metric; before=%v after=%v", before, got)
	}
}

// Scenario 3 — grandfathered/over-cap account: existing projects survive; only
// NEW creation is blocked (block-new-only).
func TestProjectCapPerAccount_Grandfathered_BlockNewOnly(t *testing.T) {
	store, std := projectCapStore(t)
	const owner, acct, tn1, tn2 = "m4-gf-uid", "acct-m4gf", "tn-m4gf-a", "tn-m4gf-b"
	projectCapSeed(t, std, owner, acct, tn1, tn2, "starter") // cap=1
	t.Cleanup(func() { projectCapCleanup(t, std, owner) })

	// Seed 2 pre-existing projects directly (grandfathered above the cap of 1).
	for _, p := range []string{"old-1", "old-2"} {
		if _, err := std.Exec(`INSERT INTO projects (id,tenant_id,name,created_by) VALUES ($1,$2,$3,$4)`, "proj-"+p, tn1, p, owner); err != nil {
			t.Fatalf("seed grandfathered project: %v", err)
		}
	}
	// New creation blocked.
	if err := mustCreate(t, store, tn1, "new-one", owner); err == nil {
		t.Fatal("grandfathered over-cap account must block NEW creation")
	}
	// Existing projects still readable (block-new-only never deletes/breaks).
	var n int
	if err := std.QueryRow(`SELECT count(*) FROM projects WHERE tenant_id IN (SELECT id FROM tenants WHERE account_id=$1)`, acct).Scan(&n); err != nil {
		t.Fatalf("count existing: %v", err)
	}
	if n != 2 {
		t.Fatalf("existing projects must survive; got %d, want 2", n)
	}
}

// Scenario 4 — concurrency: N goroutines create projects for one at-boundary
// Account; the advisory-lock atomic guard means the count never exceeds the cap.
func TestProjectCapPerAccount_ConcurrentAtBoundary_NeverExceedsCap(t *testing.T) {
	store, std := projectCapStore(t)
	const owner, acct, tn1, tn2 = "m4-cc-uid", "acct-m4cc", "tn-m4cc-a", "tn-m4cc-b"
	projectCapSeed(t, std, owner, acct, tn1, tn2, "pro") // pro cap = 10
	t.Cleanup(func() { projectCapCleanup(t, std, owner) })

	const cap = 10
	// Pre-fill to cap-1 so exactly ONE more creation may succeed.
	for i := 0; i < cap-1; i++ {
		if err := mustCreate(t, store, tn1, "pre-"+string(rune('a'+i)), owner); err != nil {
			t.Fatalf("pre-fill %d: %v", i, err)
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, capRejected, otherErr := 0, 0, 0
	for i := 0; i < 8; i++ { // 8 racers for the single remaining slot
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Half the racers go through tn2 (the other tenant of the same account).
			tn := tn1
			if i%2 == 0 {
				tn = tn2
			}
			err := mustCreate(t, store, tn, "race-"+string(rune('a'+i)), owner)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case isCapError(err):
				capRejected++ // a loser, rejected for the RIGHT reason (the cap)
			default:
				otherErr++ // a loser that failed for an UNRELATED reason — a regression
			}
		}(i)
	}
	wg.Wait()

	if ok != 1 {
		t.Fatalf("exactly ONE racer should win the last slot; %d succeeded", ok)
	}
	// The 7 losers must ALL be cap rejections — not lock-timeouts / tx errors (which
	// would mask a regression while still leaving ok==1).
	if otherErr != 0 || capRejected != 7 {
		t.Fatalf("losers must all be PROJECTS_EXCEED_PLAN; capRejected=%d otherErr=%d (want 7, 0)", capRejected, otherErr)
	}
	var n int
	if err := std.QueryRow(`SELECT count(*) FROM projects WHERE tenant_id IN (SELECT id FROM tenants WHERE account_id=$1)`, acct).Scan(&n); err != nil {
		t.Fatalf("final count: %v", err)
	}
	if n != cap {
		t.Fatalf("final project count = %d, must equal cap %d (atomic guard held)", n, cap)
	}
}
