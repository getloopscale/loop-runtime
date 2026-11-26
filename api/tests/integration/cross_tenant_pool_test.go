// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// setupCrossTenantTestDB returns a pool for tests whose FIXTURE spans tenants —
// creating a tenant and writing its rows in the same breath.
//
// It is `loop_runtime_worker`, the role whose reach is the named *_worker_cross_tenant
// policies (migration 000062), for the same reason production uses it for
// background work: the alternative is a standing bypass, which migration 000064
// removed. Falls back to DATABASE_URL when the worker DSN is unset so a machine
// without the role split still runs the suite.
func setupCrossTenantTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	pool, err := pgxpool.New(ctx, seedDBURL())
	if err != nil {
		cancel()
		// Machine without a database. In CI the integration job provisions one, so this
		// path does not run there; the "skip by default" tension is in:
		// https://github.com/getloopscale/loop-runtime/issues/289
		t.Skipf("skipping: cannot connect to PostgreSQL: %v", err)
	}

	var rlsEnabled bool
	if err := pool.QueryRow(ctx,
		"SELECT relrowsecurity FROM pg_class WHERE relname = 'builds'").Scan(&rlsEnabled); err != nil || !rlsEnabled {
		cancel()
		pool.Close()
		// Database without the isolation migrations — the fixture has nothing to exercise.
		// https://github.com/getloopscale/loop-runtime/issues/289
		t.Skipf("skipping: isolation migrations not applied (RLS off on builds)")
	}

	t.Cleanup(func() { cancel(); pool.Close() })
	return pool
}

// seedTenantForBuild creates the user/account/tenant a `builds` row needs.
//
// Migration 000061 added a trigger that resolves `builds.cell_id` from
// `tenants.cell_id` and raises when the tenant is absent. Fixtures written
// before it inserted a build for a tenant that never existed and passed; they
// now fail with the trigger's RAISE message, which says the tenant does not
// exist or has no cell. Seeding the parent rows is what the trigger asks for —
// the honest fix, rather than disabling it.
func seedTenantForBuild(t *testing.T, pool *pgxpool.Pool, tenantID string) {
	t.Helper()

	ctx := context.Background()
	owner := "u-" + tenantID
	account := "acct-" + tenantID

	for _, step := range []struct {
		what string
		sql  string
		args []any
	}{
		{"user", `INSERT INTO users (id,email,display_name,username)
		          VALUES ($1,$1||'@fixture.test','F',$1) ON CONFLICT (id) DO NOTHING`, []any{owner}},
		{"account", `INSERT INTO accounts (id,display_name,plan,owner_id)
		             VALUES ($1,'F','starter',$2) ON CONFLICT (id) DO NOTHING`, []any{account, owner}},
		{"tenant", `INSERT INTO tenants (id,display_name,slug,owner_id,account_id,cell_id)
		            VALUES ($1,$1,$1,$2,$3,'cell-0') ON CONFLICT (id) DO NOTHING`, []any{tenantID, owner, account}},
	} {
		if _, err := pool.Exec(ctx, step.sql, step.args...); err != nil {
			t.Fatalf("seed %s for %s: %v", step.what, tenantID, err)
		}
	}
}
