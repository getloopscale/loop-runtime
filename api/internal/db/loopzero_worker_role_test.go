// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

// loopzero_worker_role_test.go — T3.1 of plan 2026-06-01-p3-infra-
// observability-test-debt-plan.md v1.1 — regression tests for the
// non-owner `loopzero_worker` PG role introduced by migration
// 000023_loopzero_worker_role.up.sql.
//
// ADR D3: background loops (reconcile, convergence, reapers, etc.)
// MUST run as `loopzero_worker` so RLS policies apply (the `loopzero` owner
// bypasses RLS by design, which was the root cause of the 2026-05
// emergency disable of FORCE RLS in migration 000010).
//
// These tests prove THREE assertions per the plan's RED-first cycle:
//   1. The `loopzero_worker` role exists with CONNECT + per-table GRANTs
//      (TestWorkerRole_Exists + TestWorkerRole_GrantsInvariant_
//      AllTablesAccessible per S1).
//   2. RLS policies enforce tenant isolation for loopzero_worker queries
//      (TestRLS_WorkerRole_TenantIsolation).
//   3. The migration's guard against being run as a non-owner is
//      defense-in-depth (TestMigration_000023_OwnerOnly — text grep
//      of the migration script for the DO block).
//
// Per ADR D3, these are integration tests against a real PostgreSQL
// instance with migration 000023 applied. Skip gracefully when PG
// unreachable or the role missing.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// loopzeroWorkerDBURL reuses the same connection-string convention as
// secretL19DBURL / releasesUniqueDBURL.
func loopzeroWorkerDBURL() string {
	return secretL19DBURL()
}

func loopzeroWorkerPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, loopzeroWorkerDBURL())
	if err != nil {
		t.Skipf("T3.1 test skipped: cannot create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("T3.1 test skipped: cannot ping PG: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func requireLoopZeroWorkerRole(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loopzero_worker')`).Scan(&exists)
	if err != nil {
		t.Skipf("T3.1 test skipped: cannot query pg_roles: %v", err)
	}
	if !exists {
		t.Skipf("T3.1 test skipped: loopzero_worker role missing (run migration 000023)")
	}
}

// TestWorkerRole_Exists (T3.1 RED #1): the role is present with login
// + non-superuser attributes per migration 000023's CREATE ROLE.
func TestWorkerRole_Exists(t *testing.T) {
	pool := loopzeroWorkerPool(t)
	requireLoopZeroWorkerRole(t, pool)
	ctx := context.Background()

	var canLogin, isSuper bool
	err := pool.QueryRow(ctx,
		`SELECT rolcanlogin, rolsuper FROM pg_roles WHERE rolname = 'loopzero_worker'`).Scan(&canLogin, &isSuper)
	if err != nil {
		t.Fatalf("query pg_roles: %v", err)
	}
	if !canLogin {
		t.Error("loopzero_worker MUST have LOGIN attribute (background workers connect via this role)")
	}
	if isSuper {
		t.Error("loopzero_worker MUST NOT be superuser (the whole point: non-owner so RLS applies)")
	}
}

// TestWorkerRole_GrantsInvariant_AllTablesAccessible (S1 SHOULD TEST):
// every table in public schema that the migration GRANTed should be
// accessible via has_table_privilege checks for SELECT/INSERT/UPDATE/
// DELETE. Catches the case where a future migration creates a table
// but forgets to GRANT — the gate test will fail at CI time, not at
// production runtime under a reconcile loop.
func TestWorkerRole_GrantsInvariant_AllTablesAccessible(t *testing.T) {
	pool := loopzeroWorkerPool(t)
	requireLoopZeroWorkerRole(t, pool)
	ctx := context.Background()

	// Tables explicitly GRANTed by migration 000023. If you add a new
	// table that background loops touch, extend BOTH the migration AND
	// this list. The intentional excludes (schema_migrations) are
	// commented in the migration header.
	grantedTables := []string{
		"app_scaling_config",
		"audit_log",
		"build_apps",
		"builds",
		"cells",
		"circuit_breakers",
		"cost_counters",
		"custom_domains",
		"environment_targets",
		"github_installations",
		"github_webhook_deliveries",
		"idempotency_keys",
		"leader_generations",
		"plan_limits",
		"preview_environments",
		"project_repositories",
		"projects",
		"release_app_attestations",
		"releases",
		"secrets",
		"sse_events",
		"subscriptions",
		"tenant_members",
		"tenant_tombstones",
		"tenants",
		"tenants_pending_deletion",
		"usage_records",
		"user_identity_map",
		"users",
		"webhook_events",
		"work_queue",
	}

	for _, table := range grantedTables {
		// Skip tables that don't actually exist (drift between repo + DB).
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = 'public' AND table_name = $1
			)`, table).Scan(&exists)
		if err != nil {
			t.Errorf("cannot check existence of %s: %v", table, err)
			continue
		}
		if !exists {
			t.Logf("table %s missing in DB (migration drift?); skipping privilege check", table)
			continue
		}

		for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
			var ok bool
			query := fmt.Sprintf(
				`SELECT has_table_privilege('loopzero_worker', 'public.%s', '%s')`,
				table, privilege)
			err := pool.QueryRow(ctx, query).Scan(&ok)
			if err != nil {
				t.Errorf("has_table_privilege(%s, %s): %v", table, privilege, err)
				continue
			}
			if !ok {
				t.Errorf("loopzero_worker MUST have %s on %s (migration 000023 GRANT regression)",
					privilege, table)
			}
		}
	}
}

// TestRLS_WorkerRole_TenantIsolation: queries running as loopzero_worker
// with `app.tenant_id` set to tenant A return ONLY tenant A's rows;
// the empty-string cross-tenant path returns rows from all tenants.
// Locks the ADR D3 design invariant — splitting into worker role
// restores RLS for background loops without requiring FORCE RLS.
//
// Requires that the test connection can SET ROLE (the default psql
// user `loopzero` is a peer-grade role to loopzero_worker, NOT a member).
// On managed PG (DOKS) the operator GRANTs membership via:
//
//	GRANT loopzero_worker TO loopzero;
//
// If membership not granted, the SET ROLE fails and the test SKIPs
// rather than reporting a misleading failure.
func TestRLS_WorkerRole_TenantIsolation(t *testing.T) {
	pool := loopzeroWorkerPool(t)
	requireLoopZeroWorkerRole(t, pool)
	ctx := context.Background()

	// Check RLS policy exists on releases (migration 000009 set this up).
	var rlsEnabled bool
	err := pool.QueryRow(ctx,
		`SELECT relrowsecurity FROM pg_class WHERE relname = 'releases'`).Scan(&rlsEnabled)
	if err != nil || !rlsEnabled {
		t.Skipf("T3.1 RLS test skipped: RLS not enabled on releases (migration 000009)")
	}

	// Seed two tenants' data as the owner.
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantA := "tn-rls-A-" + suffix
	tenantB := "tn-rls-B-" + suffix

	for _, tenant := range []string{tenantA, tenantB} {
		// The migration 000061 trigger resolves builds.cell_id from the tenant
		// and fails loudly if it does not exist; the fixture predates the trigger.
		seedTenantRow(t, pool, tenant)

		buildID := "bld-rls-" + tenant
		releaseID := "rel-rls-" + tenant
		_, err := pool.Exec(ctx,
			`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
			 VALUES ($1, $2, 'proj-rls', 'production', 'SUCCEEDED', 'corr-'||$2, 'system:test')`,
			buildID, tenant)
		if err != nil {
			t.Fatalf("seed build for %s: %v", tenant, err)
		}
		_, err = pool.Exec(ctx,
			`INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by)
			 VALUES ($1, $2, $3, 'proj-rls', 'production', 'PENDING', 'corr-'||$3, 'system:test')`,
			releaseID, buildID, tenant)
		if err != nil {
			t.Fatalf("seed release for %s: %v", tenant, err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			"DELETE FROM releases WHERE tenant_id IN ($1, $2)", tenantA, tenantB)
		_, _ = pool.Exec(context.Background(),
			"DELETE FROM builds WHERE tenant_id IN ($1, $2)", tenantA, tenantB)
	})

	// Try SET ROLE inside a tx + verify SET ROLE actually succeeded
	// (SET ROLE silently no-ops when membership is missing on some
	// configs — we check current_user post-SET to confirm).
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SET LOCAL ROLE loopzero_worker"); err != nil {
		t.Skipf("T3.1 RLS test skipped: SET ROLE loopzero_worker failed (grant membership: GRANT loopzero_worker TO loopzero;): %v", err)
	}
	var currentUser string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&currentUser); err != nil {
		t.Fatalf("current_user: %v", err)
	}
	if currentUser != "loopzero_worker" {
		t.Skipf("T3.1 RLS test skipped: SET ROLE no-op (current_user=%q); grant membership: GRANT loopzero_worker TO loopzero;", currentUser)
	}

	// THE CONTRACT CHANGED IN MIGRATION 000062, and this test guarded the old one.
	//
	// The 000023 design was: the worker is filtered by the context like everyone
	// else, and it "crosses" by setting `app.tenant_id` to an EMPTY string. 000062
	// made the policies fail-closed — empty now matches NOTHING — and gave the
	// worker a NAMED policy (`releases_worker_cross_tenant`, USING true). Crossing
	// now comes from the ROLE, not from the context value.
	//
	// It is the same belief the TS products' `withCrossWorkspaceScope` carried:
	// an empty context read as "no filter", when fail-closed reads it as "no
	// rows". There it returned zero in silence; here the test asserted the worker
	// would see a single tenant — and asserting that of a role that crosses by
	// design is describing an isolation it deliberately does not have.
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantA); err != nil {
		t.Fatalf("set_config A: %v", err)
	}
	var visible []string
	rows, err := tx.Query(ctx, "SELECT tenant_id FROM releases WHERE tenant_id IN ($1, $2)", tenantA, tenantB)
	if err != nil {
		t.Fatalf("query A: %v", err)
	}
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err != nil {
			t.Fatalf("scan A: %v", err)
		}
		visible = append(visible, tid)
	}
	rows.Close()
	if len(visible) != 2 {
		t.Errorf("loopzero_worker crosses tenants through the NAMED policy from 000062, so even with "+
			"app.tenant_id=A it must see A and B; got %v — if this comes back as 1, the "+
			"releases_worker_cross_tenant policy is gone and the background loops stopped seeing", visible)
	}

	// Empty context: it is NO longer what grants the crossing (000062 removed the
	// permissive disjunct). The worker still sees both because the ROLE's policy
	// admits everything — the empty value simply stopped mattering. Kept as an
	// assertion precisely to catch a return of the old design.
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', '', true)"); err != nil {
		t.Fatalf("set_config empty: %v", err)
	}
	visible = nil
	rows, err = tx.Query(ctx, "SELECT tenant_id FROM releases WHERE tenant_id IN ($1, $2) ORDER BY tenant_id", tenantA, tenantB)
	if err != nil {
		t.Fatalf("query empty: %v", err)
	}
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err != nil {
			t.Fatalf("scan empty: %v", err)
		}
		visible = append(visible, tid)
	}
	rows.Close()
	if len(visible) != 2 {
		t.Errorf("with app.tenant_id='' the worker keeps seeing both — crossing comes from the role, "+
			"not from the empty value; got %v", visible)
	}
}

// TestMigration_000023_OwnerOnly (pure-source-grep): the migration
// includes a DO block asserting current_user='loopzero' before CREATE ROLE.
// Catches future refactors that might drop the guard. Test does NOT
// require PG (pure file read).
func TestMigration_000023_OwnerOnly(t *testing.T) {
	// File lives at api/internal/db/migrations/000023_loopzero_worker_role.up.sql
	// relative to repo root; this test runs from api/internal/db/ so
	// resolve via os.Getwd.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	path := cwd + "/migrations/000023_loopzero_worker_role.up.sql"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	src := string(data)
	// Must contain the owner-guard DO block.
	if !strings.Contains(src, "current_user <> 'loopzero'") {
		t.Error("migration 000023 MUST guard against running as non-owner (RAISE EXCEPTION when current_user <> 'loopzero')")
	}
	if !strings.Contains(src, "RAISE EXCEPTION") {
		t.Error("migration 000023 owner-guard MUST RAISE EXCEPTION (not just log)")
	}
	// Must NOT include an EXECUTABLE DROP ROLE statement (forward-only).
	// Lines that are pure comments (starting with `--`) MAY mention the
	// conceptual rollback for operator runbook clarity; we strip those
	// before checking.
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue // SQL comment line — documentation is allowed.
		}
		if strings.Contains(trimmed, "DROP ROLE") {
			t.Errorf("migration 000023 MUST NOT contain executable DROP ROLE (forward-only); found: %q", trimmed)
		}
	}
}
