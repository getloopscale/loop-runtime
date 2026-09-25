// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/store"
)

// The plan every gate enforces is read by store.PlanLimitsStore, and
// `subscriptions` and `tenants` are under fail-closed Row Level Security
// (migration 000062). These tests pin which connection can read it: the
// cross-tenant worker role can, the request role cannot — which is why the
// composition root builds the store on the background pool.
//
//	DATABASE_URL=<loopzero_api> LOOPZERO_WORKER_DATABASE_URL=<loopzero_worker> \
//	  go test ./tests/integration/ -run PlanLimitsStore -v
func planLimitsSeed(t *testing.T, owner, acct, tn1, tn2, plan string) {
	t.Helper()
	_ = testPool(t) // skips when PostgreSQL is unreachable
	seed, err := sql.Open("pgx", seedDBURL())
	if err != nil {
		t.Fatalf("open seed DB: %v", err)
	}
	t.Cleanup(func() { _ = seed.Close() })
	projectCapSeed(t, seed, owner, acct, tn1, tn2, plan)
	t.Cleanup(func() { projectCapCleanup(t, seed, owner) })
}

// workerPool is the cross-tenant pool main builds the plan limiter on.
func workerPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return poolFor(t, seedDBURL())
}

func planLimitsStoreOn(t *testing.T, url string) *store.PlanLimitsStore {
	t.Helper()
	return store.NewPlanLimitsStore(&db.PgxAdapter{Pool: poolFor(t, url)})
}

func TestPlanLimitsStoreOnTheWorkerRoleReadsTheTenantPlan(t *testing.T) {
	const owner, acct, tn1, tn2 = "rls-plan-uid", "acct-rls-plan", "tn-rls-plan-a", "tn-rls-plan-b"
	planLimitsSeed(t, owner, acct, tn1, tn2, "pro")
	limits := planLimitsStoreOn(t, seedDBURL())

	plan, err := limits.GetTenantPlan(context.Background(), tn1)

	if err != nil || plan != "pro" {
		t.Fatalf("tenant plan = %q, %v; want pro", plan, err)
	}
}

// The Account's single subscription sits on tn1; tn2 is a sibling tenant of
// the same Account. Its Account plan is another tenant's row.
func TestPlanLimitsStoreOnTheWorkerRoleReadsTheAccountPlanOfASiblingTenant(t *testing.T) {
	const owner, acct, tn1, tn2 = "rls-acct-uid", "acct-rls-acct", "tn-rls-acct-a", "tn-rls-acct-b"
	planLimitsSeed(t, owner, acct, tn1, tn2, "team")
	limits := planLimitsStoreOn(t, seedDBURL())

	plan, err := limits.GetAccountPlanByTenant(context.Background(), tn2)

	if err != nil || plan != "team" {
		t.Fatalf("account plan through the sibling tenant = %q, %v; want team", plan, err)
	}
}

// The request role is the one the plan lookup must NOT run on: with no tenant
// scope, RLS hides the subscription and the tenant reads as Starter. This is
// the failure the background-pool wiring exists to prevent.
func TestPlanLimitsStoreOnTheRequestRoleCannotSeeTheSubscription(t *testing.T) {
	_ = testPool(t) // skips when PostgreSQL is unreachable
	requestPool := poolFor(t, dbURL())
	var bypasses bool
	if err := requestPool.QueryRow(context.Background(),
		"SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user").Scan(&bypasses); err != nil {
		t.Fatalf("read the request role's RLS posture: %v", err)
	}
	if bypasses {
		t.Skip("DATABASE_URL is not the isolated request role (it bypasses RLS); point it at loopzero_api")
	}
	const owner, acct, tn1, tn2 = "rls-req-uid", "acct-rls-req", "tn-rls-req-a", "tn-rls-req-b"
	planLimitsSeed(t, owner, acct, tn1, tn2, "pro")
	limits := planLimitsStoreOn(t, dbURL())

	plan, err := limits.GetTenantPlan(context.Background(), tn1)

	if err != nil || plan != "" {
		t.Fatalf("tenant plan over the request role = %q, %v; want \"\" (hidden by RLS)", plan, err)
	}
}

// plan_limits carries no tenant column and no RLS; these pin the adapter's
// NULL and missing-row mapping against the real column types.
func TestPlanLimitsStoreMapsNullCapsAndAMissingRow(t *testing.T) {
	_ = testPool(t)
	seed, err := sql.Open("pgx", seedDBURL())
	if err != nil {
		t.Fatalf("open seed DB: %v", err)
	}
	t.Cleanup(func() { _ = seed.Close() })
	const plan, metric = "rls-test-plan", "rls-test-metric"
	if _, err := seed.Exec(`INSERT INTO plan_limits (plan, metric, hard_limit, soft_limit) VALUES ($1, $2, NULL, NULL)`, plan, metric); err != nil {
		t.Fatalf("seed plan_limits: %v", err)
	}
	t.Cleanup(func() { _, _ = seed.Exec(`DELETE FROM plan_limits WHERE plan = $1`, plan) })
	limits := planLimitsStoreOn(t, seedDBURL())

	hard, soft, err := limits.GetLimits(context.Background(), plan, metric)
	if err != nil || hard != -1 || soft != 0 {
		t.Fatalf("NULL caps = (%d, %d, %v); want (-1, 0, nil)", hard, soft, err)
	}
	hard, soft, err = limits.GetLimits(context.Background(), plan, "no-such-metric")
	if err != nil || hard != 0 || soft != 0 {
		t.Fatalf("missing row = (%d, %d, %v); want (0, 0, nil)", hard, soft, err)
	}
}
