// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package integration

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The per-account project cap counts across ALL of an Account's tenants:
//
//	SELECT count(*) FROM projects
//	 WHERE tenant_id IN (SELECT id FROM tenants WHERE account_id = $1)
//
// Until 2026-08-14 the request pool held BYPASSRLS, so the policy never applied
// and the count was complete. Migration 000062 plus the boot guard moved that
// pool to loopruntime_api, and the same query started returning only the tenant whose
// context the transaction carries — turning a per-ACCOUNT cap into a per-TENANT
// one. A plan capped at N projects would allow N per tenant, without limit.
//
// This test pins the property that broke, at the layer where it broke: the same
// SQL, over the two connections, must disagree — and the one the cap uses must
// be the one that sees the whole account.
//
// @see internal/db/store/project_wrapper.go — enforceProjectCapTx / crossTenant
func TestProjectCap_CrossTenantCount(t *testing.T) {
	ctx := context.Background()

	worker := poolFor(t, dsnOrSkip(t, "LOOPRUNTIME_WORKER_DATABASE_URL"))
	request := poolFor(t, dsnOrSkip(t, "LOOPRUNTIME_API_DATABASE_URL"))

	const (
		acct = "acct-xtenant"
		own  = "u-xtenant"
		tnA  = "tn-xtenant-a"
		tnB  = "tn-xtenant-b"
	)

	// The fixture is cross-tenant work, so it runs on the connection whose reach
	// is the named worker policy — the same rule production follows.
	seedCrossTenantAccount(t, ctx, worker, acct, own, tnA, tnB)
	t.Cleanup(func() { cleanCrossTenantAccount(ctx, worker, acct, own, tnA, tnB) })

	const countSQL = `SELECT count(*) FROM projects
	                   WHERE tenant_id IN (SELECT id FROM tenants WHERE account_id = $1)`

	// The request path always carries exactly one tenant's context.
	conn, err := request.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire request connection: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT set_config('app.tenant_id', $1, false)`, tnA); err != nil {
		t.Fatalf("set tenant context: %v", err)
	}

	var scoped, crossTenant int
	if err := conn.QueryRow(ctx, countSQL, acct).Scan(&scoped); err != nil {
		t.Fatalf("count on the request connection: %v", err)
	}
	if err := worker.QueryRow(ctx, countSQL, acct).Scan(&crossTenant); err != nil {
		t.Fatalf("count on the cross-tenant connection: %v", err)
	}

	if crossTenant != 2 {
		t.Fatalf("the account has 2 projects across 2 tenants; the cross-tenant count read %d — "+
			"the connection the cap relies on cannot see the account", crossTenant)
	}
	if scoped != 1 {
		t.Fatalf("expected the request connection to see only its own tenant (1), got %d — "+
			"if this is 2 the request pool is crossing tenants and AssertRequestPoolIsolated should have refused to boot", scoped)
	}

	// The gap IS the defect. Stating it as an assertion keeps the reason for
	// `crossTenant` legible: remove that field and the cap silently counts
	// `scoped`, which is under the cap while the account is over it.
	if crossTenant <= scoped {
		t.Fatalf("cross-tenant=%d scoped=%d — with no gap this test proves nothing", crossTenant, scoped)
	}
}

func seedCrossTenantAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, acct, owner, tnA, tnB string) {
	t.Helper()
	cleanCrossTenantAccount(ctx, pool, acct, owner, tnA, tnB)

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id,email,display_name,username) VALUES ($1,$1||'@xtenant.test','O',$1)
	      ON CONFLICT (id) DO NOTHING`, owner)
	exec(`INSERT INTO accounts (id,display_name,plan,owner_id) VALUES ($1,'X','starter',$2)
	      ON CONFLICT (id) DO NOTHING`, acct, owner)
	for _, tn := range []string{tnA, tnB} {
		exec(`INSERT INTO tenants (id,display_name,slug,owner_id,account_id,cell_id)
		      VALUES ($1,$1,$1,$2,$3,'cell-0') ON CONFLICT (id) DO NOTHING`, tn, owner, acct)
		exec(`INSERT INTO projects (id,tenant_id,name,created_by)
		      VALUES ('p-'||$1,$1,'p-'||$1,$2) ON CONFLICT (id) DO NOTHING`, tn, owner)
	}
}

func cleanCrossTenantAccount(ctx context.Context, pool *pgxpool.Pool, acct, owner, tnA, tnB string) {
	for _, q := range []string{
		`DELETE FROM projects WHERE tenant_id = ANY($1)`,
		`DELETE FROM tenant_members WHERE tenant_id = ANY($1)`,
		`DELETE FROM tenants WHERE id = ANY($1)`,
	} {
		_, _ = pool.Exec(ctx, q, []string{tnA, tnB})
	}
	_ = execAsAccountsOwner(ctx, `DELETE FROM accounts WHERE id = $1`, acct)
	_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, owner)
}
