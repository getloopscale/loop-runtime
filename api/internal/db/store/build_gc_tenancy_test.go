// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

func seedGCTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) {
	t.Helper()
	u, a := "u-"+tenantID, "a-"+tenantID
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM builds WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM tenants WHERE id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, a)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, u)
	}
	cleanup()
	t.Cleanup(cleanup)
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users(id,email,username) VALUES ($1,$2,$3)`, []any{u, tenantID + "@t.io", tenantID}},
		{`INSERT INTO accounts(id,display_name,owner_id) VALUES ($1,$2,$3)`, []any{a, tenantID, u}},
		{`INSERT INTO tenants(id,slug,owner_id,account_id,cell_id) VALUES ($1,$1,$2,$3,'cell-0')`, []any{tenantID, u, a}},
	} {
		if _, err := pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("seed %q: %v", q.sql, err)
		}
	}
}

func seedOldBuilds(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, ageDays ...int) {
	t.Helper()
	for i, age := range ageDays {
		if _, err := pool.Exec(ctx, `INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by, created_at)
		        VALUES ($1,$2,'web','production','SUCCEEDED','c',$3, now() - make_interval(days => $4))`,
			fmt.Sprintf("b-%s-%d", tenantID, i), tenantID, "u-"+tenantID, age); err != nil {
			t.Fatalf("seed build: %v", err)
		}
	}
}

func countBuilds(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM builds WHERE tenant_id=$1`, tenantID).Scan(&n); err != nil {
		t.Fatalf("count builds: %v", err)
	}
	return n
}

// builds.project_id holds the project NAME, unique only within a tenant. The
// retention window keeps the newest N builds per project+environment OF A
// TENANT: two tenants that both have a "web" project must not share one
// budget, or one tenant's recent deploys delete the other's retained history.
func TestBuildGCStore_RetentionIsPerTenant(t *testing.T) {
	ctx, pool := meteringPool(t)
	const older, newer = "tn-gc-older", "tn-gc-newer"
	seedGCTenant(t, ctx, pool, older)
	seedGCTenant(t, ctx, pool, newer)
	seedOldBuilds(t, ctx, pool, older, 50, 49, 48)
	seedOldBuilds(t, ctx, pool, newer, 40, 39, 38)

	s := &PostgresBuildGCStore{DB: &db.PgxAdapter{Pool: pool}}
	if _, err := s.DeleteOldBuilds(ctx, 30*24*time.Hour, 2); err != nil {
		t.Fatalf("DeleteOldBuilds: %v", err)
	}

	for _, tn := range []string{older, newer} {
		if got := countBuilds(t, ctx, pool, tn); got != 2 {
			t.Errorf("tenant %s kept %d builds of project web/production, want the 2 it is owed", tn, got)
		}
	}
}
