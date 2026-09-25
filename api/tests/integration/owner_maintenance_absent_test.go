// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// The owner holds no standing cross-tenant bypass.
//
// Migration 000062 shipped a `<table>_owner_maintenance` policy on all 30
// tenant-scoped tables plus `tenants`, each `TO CURRENT_USER USING (true) WITH
// CHECK (true)`. It was written to keep test seeds and migrations working once
// FORCE ROW LEVEL SECURITY started applying to the table owner, on the reasoning
// that FORCE against an owner is not a boundary anyway — the owner holds DDL and
// could lift it.
//
// That reasoning was wrong in the way that matters. Lifting FORCE is a deliberate
// DDL statement somebody would have to write and a reviewer could see; a standing
// `USING (true)` is a bypass on every query, silently, forever. The two are not
// equivalent, and the second is exactly the shape of privilege this whole change
// exists to remove.
//
// It was also unnecessary. The isolation policy is created with no `TO` clause,
// which makes it `TO PUBLIC` — it already covers the owner. Under FORCE the owner
// is simply treated like any other role: set the context and it works, and
// `tenantScopedPool` sets it on every connection it hands out. The maintenance
// policy added a second, permanent, unnamed way across tenants to solve a problem
// the first policy did not have.
//
// And `TO CURRENT_USER` resolves at CREATE POLICY time to the literal role name
// that ran the migration, so the policies were also non-deterministic across
// environments: whoever migrated got the bypass.
//
// Removed by migration 000064. This test is what keeps them removed.
//
//	LOOPZERO_API_DATABASE_URL=postgres://loopzero_api:...@localhost:5432/loopzero \
//	LOOPZERO_RLS_AUDIT_REQUIRE=1 go test ./tests/integration/ -run OwnerMaintenance -v
package integration

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// openAnyRole connects with whichever DSN is available. The property under test
// is a catalogue fact, identical from every role, so the test does not care which
// one answers — only that something does.
func openAnyRole(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	for _, env := range []string{
		"LOOPZERO_API_DATABASE_URL",
		"LOOPZERO_RLS_AUDIT_SUPERUSER_URL",
		"LOOPZERO_WORKER_DATABASE_URL",
		"DATABASE_URL",
	} {
		dsn := os.Getenv(env)
		if dsn == "" {
			continue
		}
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := db.PingContext(ctx); err != nil {
			cancel()
			_ = db.Close()
			continue
		}
		t.Cleanup(func() { cancel(); _ = db.Close() })
		return ctx, db
	}
	if auditRequired() {
		t.Fatal("no reachable database DSN, and LOOPZERO_RLS_AUDIT_REQUIRE=1 forbids skipping " +
			"— a green run here would assert nothing about the owner bypass")
	}
	// Skipping here is the right decision on a machine without a database, and the WRONG
	// decision in CI: an isolation control that reports green without being exercised is the
	// very failure this test exists to detect. CI sets LOOPZERO_RLS_AUDIT_REQUIRE=1, so there it
	// becomes a failure. The tension is tracked in:
	// https://github.com/getloopscale/loopzero/issues/289
	t.Skip("no reachable database DSN (set LOOPZERO_RLS_AUDIT_REQUIRE=1 to make this a failure)")
	return nil, nil
}

// TestOwnerMaintenancePoliciesAreGone fails if migration 000062's owner bypass
// is reintroduced, under that name or any other standing USING(true) granted to
// the migrating role.
func TestOwnerMaintenancePoliciesAreGone(t *testing.T) {
	ctx, db := openAnyRole(t)

	rows, err := db.QueryContext(ctx, `
		SELECT tablename, policyname
		  FROM pg_policies
		 WHERE schemaname = 'public'
		   AND policyname LIKE '%_owner_maintenance'
		 ORDER BY tablename`)
	if err != nil {
		t.Fatalf("reading pg_policies: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var found []string
	for rows.Next() {
		var table, policy string
		if err := rows.Scan(&table, &policy); err != nil {
			t.Fatalf("scan: %v", err)
		}
		found = append(found, table+"."+policy)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}

	if len(found) > 0 {
		t.Fatalf("the owner's standing cross-tenant bypass is back on %d tables: %s\n"+
			"The isolation policy has no TO clause, so it is TO PUBLIC and already covers the "+
			"owner — it only has to set app.workspace_id, which tenantScopedPool does. A "+
			"USING(true) policy here is a second, permanent, unnamed way across every tenant.",
			len(found), strings.Join(found, ", "))
	}
}

// TestNoUnconditionalTruePolicyOutsideNamedWorkers is the wider net. Catching the
// name alone would let the same privilege return as `*_admin_override`; what makes
// the worker and cloud policies acceptable is that each names a role in `TO` and
// appears in pg_policy under that name. A permissive USING(true) applying to
// PUBLIC is a bypass for everyone, whatever it is called.
func TestNoUnconditionalTruePolicyOutsideNamedWorkers(t *testing.T) {
	ctx, db := openAnyRole(t)

	rows, err := db.QueryContext(ctx, `
		SELECT tablename, policyname, COALESCE(array_to_string(roles, ','), '')
		  FROM pg_policies
		 WHERE schemaname = 'public'
		   AND COALESCE(qual, 'true') = 'true'
		   AND 'public' = ANY(roles)
		 ORDER BY tablename`)
	if err != nil {
		t.Fatalf("reading pg_policies: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var offenders []string
	for rows.Next() {
		var table, policy, roles string
		if err := rows.Scan(&table, &policy, &roles); err != nil {
			t.Fatalf("scan: %v", err)
		}
		offenders = append(offenders, table+"."+policy+" TO "+roles)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf("policies granting unconditional access to PUBLIC: %s\n"+
			"Cross-tenant reach is allowed, but only when it NAMES the role it is for "+
			"(the *_worker_cross_tenant and *_cloud_cross_tenant policies do). Granted to "+
			"PUBLIC it applies to every role, which is not a policy — it is the absence of one.",
			strings.Join(offenders, ", "))
	}
}
