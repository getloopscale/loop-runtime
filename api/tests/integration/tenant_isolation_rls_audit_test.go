// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Tenant isolation — the enforcement contract.
//
// These tests assert the state migration 000062 establishes: the database
// itself refuses cross-tenant reads and writes, instead of relying on every
// query carrying a correct WHERE clause.
//
// WHY THIS FILE EXISTS SEPARATELY from database_hardening_test.go: that file's
// TestRLSTenantIsolation asserted isolation over a connection that could not be
// isolated (dbURL() is the owner, and before 000062 the owner bypassed every
// policy). It also never ran — ci.yml had no `services:` block, so
// setupHardeningTestDB skipped on an unreachable database and the suite stayed
// green for five months over an untested control. Both are fixed; this file is
// the regression protector that keeps them fixed.
//
// Three roles, three different jobs — the tests use the same split as production:
//
//	DATABASE_URL              owner        DDL and fixtures only, never a request
//	LOOPRUNTIME_API_DATABASE_URL     loopruntime_api     the HTTP request path — must be isolated
//	LOOPRUNTIME_WORKER_DATABASE_URL  loopruntime_worker  background loops — cross-tenant by policy
//
// Set LOOPRUNTIME_RLS_AUDIT_REQUIRE=1 (CI, compliance runs) and a missing database or
// missing DSN is a FAILURE, not a skip. A control that reports green because it
// did not run is worse than no control: it produces an audit artefact asserting
// a property nobody measured.
//
//	DATABASE_URL=postgres://loopruntime:loopruntime@localhost:55432/loopruntime_test?sslmode=disable \
//	LOOPRUNTIME_API_DATABASE_URL=postgres://loopruntime_api:x@localhost:55432/loopruntime_test?sslmode=disable \
//	LOOPRUNTIME_WORKER_DATABASE_URL=postgres://loopruntime_worker:w@localhost:55432/loopruntime_test?sslmode=disable \
//	LOOPRUNTIME_RLS_AUDIT_REQUIRE=1 \
//	go test ./tests/integration/ -run TenantIsolation -count=1 -v
package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// auditRequired reports whether a missing database must fail instead of skip.
func auditRequired() bool { return os.Getenv("LOOPRUNTIME_RLS_AUDIT_REQUIRE") == "1" }

// auditPool opens the DSN in envVar, or fails/skips per LOOPRUNTIME_RLS_AUDIT_REQUIRE.
func auditPool(t *testing.T, envVar, role string) (context.Context, *pgxpool.Pool) {
	t.Helper()

	dsn := os.Getenv(envVar)
	if dsn == "" {
		if envVar == "DATABASE_URL" {
			dsn = dbURL() // shared default with the rest of the suite
		} else if auditRequired() {
			t.Fatalf("LOOPRUNTIME_RLS_AUDIT_REQUIRE=1 but %s is unset — the %s path would go unmeasured", envVar, role)
		} else {
			// Skipped when the role's DSN is absent, which is the honest answer —
			// but it means this compliance evidence is NOT collected by default.
			// Tracked: https://github.com/getloopscale/loop-runtime/issues/289
			t.Skipf("%s not set — %s path skipped (set LOOPRUNTIME_RLS_AUDIT_REQUIRE=1 to make this a failure)", envVar, role)
		}
	}

	ctx := context.Background()
	connCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(connCtx, dsn)
	if err == nil {
		err = pool.Ping(connCtx)
	}
	if err != nil {
		if auditRequired() {
			t.Fatalf("LOOPRUNTIME_RLS_AUDIT_REQUIRE=1 and the %s connection failed: %v\n"+
				"This test is compliance evidence — a skip here would report a tenant-isolation "+
				"control as green without measuring it.", role, err)
		}
		// Same shape: unreachable role degrades to skip unless REQUIRE is set.
		// Tracked: https://github.com/getloopscale/loop-runtime/issues/289
		t.Skipf("cannot connect as %s (%v)", role, err)
	}
	t.Cleanup(pool.Close)
	return ctx, pool
}

// rlsPosture is the measured privilege posture of a connection.
type rlsPosture struct {
	role      string
	bypassRLS bool
	isOwner   bool
	forced    bool
}

func (p rlsPosture) String() string {
	return fmt.Sprintf("role=%s rolbypassrls=%v owns_builds=%v force_rls=%v",
		p.role, p.bypassRLS, p.isOwner, p.forced)
}

// canBeIsolated reports whether RLS can bind at all on this connection.
// Both escapes must be closed: BYPASSRLS short-circuits the policy, and an
// owner bypasses its own tables unless FORCE is on.
func (p rlsPosture) canBeIsolated() bool { return !p.bypassRLS && (!p.isOwner || p.forced) }

// measurePosture reads the live catalog. Everything here is observed, never assumed.
func measurePosture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) rlsPosture {
	t.Helper()
	var p rlsPosture
	if err := pool.QueryRow(ctx,
		`SELECT current_user, rolbypassrls FROM pg_roles WHERE rolname = current_user`,
	).Scan(&p.role, &p.bypassRLS); err != nil {
		t.Fatalf("measure role: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT pg_get_userbyid(relowner) = current_user, relforcerowsecurity
		   FROM pg_class WHERE relname = 'builds'`,
	).Scan(&p.isOwner, &p.forced); err != nil {
		t.Fatalf("measure table: %v", err)
	}
	return p
}

// seedTwoTenants creates two tenants, each with one build.
//
// The builds table carries a trigger that resolves cell_id from the tenant, so
// the fixture has to build the real chain — user → account → cell → tenant →
// build — rather than inserting an orphan row. Rows on tenant-scoped tables are
// written under their own tenant context, which doubles as a check that the
// policy's WITH CHECK clause admits a correctly scoped INSERT.
//
// The non-tenant-scoped links (users, accounts, cells) carry no policy and are
// written directly.
func seedTwoTenants(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string, string, string) {
	t.Helper()
	stamp := time.Now().UnixNano()
	tenantA := fmt.Sprintf("iso-tenant-a-%d", stamp)
	tenantB := fmt.Sprintf("iso-tenant-b-%d", stamp)
	buildA := fmt.Sprintf("iso-bld-a-%d", stamp)
	buildB := fmt.Sprintf("iso-bld-b-%d", stamp)
	userID := fmt.Sprintf("iso-user-%d", stamp)
	acctID := fmt.Sprintf("iso-acct-%d", stamp)
	// cells.cell_id is constrained to ^cell-[a-z0-9-]+$
	cellID := fmt.Sprintf("cell-iso-%d", stamp)

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed %s: %v", what, err)
		}
	}
	mustExec("user", `INSERT INTO users (id, email, username) VALUES ($1, $1||'@example.test', $1)`, userID)
	mustExec("account", `INSERT INTO accounts (id, display_name, owner_id) VALUES ($1, 'iso-account', $2)`, acctID, userID)
	mustExec("cell", `INSERT INTO cells (cell_id, display_name) VALUES ($1, 'iso-cell')`, cellID)

	for _, row := range []struct{ build, tenant string }{{buildA, tenantA}, {buildB, tenantB}} {
		if err := withTenant(ctx, pool, row.tenant, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx,
				`INSERT INTO tenants (id, display_name, slug, owner_id, account_id, cell_id)
				 VALUES ($1, $1, $1, $2, $3, $4)`,
				row.tenant, userID, acctID, cellID); err != nil {
				return fmt.Errorf("tenant row: %w", err)
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
				 VALUES ($1, $2, 'proj-iso', 'production', 'QUEUED', 'corr-iso', 'rls-audit')`,
				row.build, row.tenant); err != nil {
				return fmt.Errorf("build row: %w", err)
			}
			return nil
		}); err != nil {
			t.Fatalf("seed %s under its own tenant context: %v\n"+
				"A correctly scoped INSERT must be admitted by the policy's WITH CHECK clause.", row.tenant, err)
		}
	}

	t.Cleanup(func() {
		cctx := context.Background()
		for _, row := range []struct{ build, tenant string }{{buildA, tenantA}, {buildB, tenantB}} {
			_ = withTenant(cctx, pool, row.tenant, func(tx pgx.Tx) error {
				_, _ = tx.Exec(cctx, `DELETE FROM builds WHERE id = $1`, row.build)
				_, _ = tx.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, row.tenant)
				return nil
			})
		}
		_, _ = pool.Exec(cctx, `DELETE FROM cells WHERE cell_id = $1`, cellID)
		_ = execAsAccountsOwner(cctx, `DELETE FROM accounts WHERE id = $1`, acctID)
		_, _ = pool.Exec(cctx, `DELETE FROM users WHERE id = $1`, userID)
	})
	return tenantA, tenantB, buildA, buildB
}

// withTenant runs fn inside a transaction whose app.tenant_id is set to tenant —
// exactly what internal/lock/environment_lock.go:88 does per request. An empty
// tenant runs the transaction with no context set at all.
func withTenant(ctx context.Context, pool *pgxpool.Pool, tenant string, fn func(pgx.Tx) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if tenant != "" {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant); err != nil {
			return err
		}
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// visibleTenants returns the tenant_ids readable for the two seeded builds,
// as the given tenant context.
func visibleTenants(t *testing.T, ctx context.Context, pool *pgxpool.Pool, asTenant, buildA, buildB string) []string {
	t.Helper()
	var seen []string
	err := withTenant(ctx, pool, asTenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT tenant_id FROM builds WHERE id IN ($1, $2) ORDER BY tenant_id`, buildA, buildB)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var tid string
			if err := rows.Scan(&tid); err != nil {
				return err
			}
			seen = append(seen, tid)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read as %q: %v", asTenant, err)
	}
	return seen
}

// requestPool opens the HTTP request path role and refuses to run the suite
// over a connection RLS cannot bind to — measuring isolation on a bypassing
// role would be theatre, which is precisely how the previous control passed.
func requestPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx, pool := auditPool(t, "LOOPRUNTIME_API_DATABASE_URL", "request path (loopruntime_api)")
	posture := measurePosture(t, ctx, pool)
	if !posture.canBeIsolated() {
		t.Fatalf("the request-path role cannot be isolated: %s\n"+
			"  rolbypassrls=true  -> the policy is never evaluated (ALTER ROLE ... NOBYPASSRLS)\n"+
			"  owner without FORCE -> the owner bypasses its own tables (migration 000062 sets FORCE)\n"+
			"Handlers must connect as a non-owner role; see db.AssertRequestPoolIsolated.", posture)
	}
	t.Logf("request-path posture: %s", posture)
	return ctx, pool
}

// TestTenantIsolation_ScopedRead_SeesOnlyOwnTenant is the core contract:
// with the tenant context established, a request reads its own rows and
// nothing else. This is the test that FAILED before migration 000062.
func TestTenantIsolation_ScopedRead_SeesOnlyOwnTenant(t *testing.T) {
	ctx, pool := requestPool(t)
	tenantA, tenantB, buildA, buildB := seedTwoTenants(t, ctx, pool)

	seen := visibleTenants(t, ctx, pool, tenantA, buildA, buildB)

	for _, tid := range seen {
		if tid == tenantB {
			t.Errorf("CROSS-TENANT READ: connection scoped to %s read a row belonging to %s (saw %v).\n"+
				"posture: %s", tenantA, tenantB, seen, measurePosture(t, ctx, pool))
		}
	}
	if len(seen) != 1 || (len(seen) == 1 && seen[0] != tenantA) {
		t.Errorf("expected exactly [%s] visible, got %v", tenantA, seen)
	}
}

// TestTenantIsolation_MissingContext_SeesNothing pins the fail-closed direction.
//
// Before 000062 the policies carried `OR current_setting(...) IS NULL OR ... = ”`,
// so a code path that forgot to set the context read EVERY tenant with no error
// and no log line. The correct behaviour is the opposite: read nothing. A caller
// that sees zero rows has a visible bug; a caller that silently sees every tenant
// does not.
func TestTenantIsolation_MissingContext_SeesNothing(t *testing.T) {
	ctx, pool := requestPool(t)
	_, _, buildA, buildB := seedTwoTenants(t, ctx, pool)

	seen := visibleTenants(t, ctx, pool, "", buildA, buildB)

	if len(seen) != 0 {
		t.Errorf("FAIL-OPEN: with app.tenant_id unset, the request role still read %d row(s): %v.\n"+
			"The policy must not carry `IS NULL` / `= ''` disjuncts — they return every tenant.", len(seen), seen)
	}
}

// TestTenantIsolation_CannotWriteForAnotherTenant covers the write direction.
// A read-only check would leave the more damaging case unmeasured: a request
// scoped to tenant A must not be able to CREATE or MUTATE rows owned by B.
func TestTenantIsolation_CannotWriteForAnotherTenant(t *testing.T) {
	ctx, pool := requestPool(t)
	tenantA, tenantB, buildA, buildB := seedTwoTenants(t, ctx, pool)

	// WITH CHECK, measured on a table with no BEFORE-INSERT trigger in the way.
	// audit_log is tenant-scoped and append-only, so the only thing that can
	// reject this row is the policy itself.
	t.Run("INSERT for another tenant is rejected by the policy", func(t *testing.T) {
		err := withTenant(ctx, pool, tenantA, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx,
				`INSERT INTO audit_log (tenant_id, action, actor, resource_type, resource_id, correlation_id)
				 VALUES ($1, 'FORGED', 'rls-audit', 'test', 'forged-id', 'corr-forged')`,
				tenantB)
			return err
		})
		if err == nil {
			t.Errorf("a request scoped to %s wrote an audit row owned by %s — WITH CHECK is missing or permissive", tenantA, tenantB)
			return
		}
		if !strings.Contains(strings.ToLower(err.Error()), "row-level security") {
			t.Errorf("INSERT was rejected, but not by RLS: %v", err)
		}
	})

	// Same attempt against `builds`, where a BEFORE-INSERT trigger resolves
	// cell_id from the tenant. The write is refused earlier and by a different
	// mechanism: the trigger's own lookup runs under tenant A's context, so it
	// cannot see tenant B and fails first. Worth pinning explicitly — the
	// isolation holds, but not via the clause a reader would expect, and a
	// future change to that trigger must not turn the refusal into a success.
	t.Run("INSERT for another tenant creates nothing, whichever layer refuses", func(t *testing.T) {
		forged := fmt.Sprintf("iso-bld-forged-%d", time.Now().UnixNano())
		err := withTenant(ctx, pool, tenantA, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx,
				`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
				 VALUES ($1, $2, 'proj-iso', 'production', 'QUEUED', 'corr-iso', 'rls-audit')`,
				forged, tenantB)
			return err
		})
		if err == nil {
			t.Errorf("a request scoped to %s inserted a build owned by %s", tenantA, tenantB)
		}

		// The refusal must also mean the row does not exist — checked with
		// cross-tenant reach so a policy-filtered read cannot fake success.
		wctx, worker := auditPool(t, "LOOPRUNTIME_WORKER_DATABASE_URL", "worker (loopruntime_worker)")
		var found int
		if err := worker.QueryRow(wctx, `SELECT count(*) FROM builds WHERE id = $1`, forged).Scan(&found); err != nil {
			t.Fatalf("verify absence: %v", err)
		}
		if found != 0 {
			t.Errorf("the forged build exists despite the error — %d row(s) present", found)
		}
	})

	t.Run("UPDATE of another tenant's row affects nothing", func(t *testing.T) {
		var affected int64
		if err := withTenant(ctx, pool, tenantA, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `UPDATE builds SET status = 'FAILED' WHERE id = $1`, buildB)
			if err != nil {
				return err
			}
			affected = tag.RowsAffected()
			return nil
		}); err != nil {
			t.Fatalf("update: %v", err)
		}
		if affected != 0 {
			t.Errorf("a request scoped to %s updated %d row(s) of %s", tenantA, affected, tenantB)
		}
	})

	t.Run("DELETE of another tenant's row affects nothing", func(t *testing.T) {
		var affected int64
		if err := withTenant(ctx, pool, tenantA, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `DELETE FROM builds WHERE id = $1`, buildB)
			if err != nil {
				return err
			}
			affected = tag.RowsAffected()
			return nil
		}); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if affected != 0 {
			t.Errorf("a request scoped to %s deleted %d row(s) of %s", tenantA, affected, tenantB)
		}
	})

	_ = buildA
}

// TestTenantIsolation_WorkerRole_KeepsCrossTenantReach guards the other side of
// the change. Reconcile loops, reapers and metering legitimately scan every
// tenant; before 000062 they did it by exploiting the fail-open disjunct. They
// now do it through a named policy granted only to loopruntime_worker.
//
// If this test fails, the isolation work broke the background loops — which is
// the failure mode that made migration 000012 disable FORCE in the first place.
func TestTenantIsolation_WorkerRole_KeepsCrossTenantReach(t *testing.T) {
	ctx, owner := auditPool(t, "DATABASE_URL", "owner")
	_, _, buildA, buildB := seedTwoTenants(t, ctx, owner)

	wctx, worker := auditPool(t, "LOOPRUNTIME_WORKER_DATABASE_URL", "worker (loopruntime_worker)")

	posture := measurePosture(t, wctx, worker)
	if posture.bypassRLS {
		t.Fatalf("loopruntime_worker has BYPASSRLS (%s) — cross-tenant reach must come from the named "+
			"policy, not an instance-wide privilege that also covers tables nobody reviewed", posture)
	}

	seen := visibleTenants(t, wctx, worker, "", buildA, buildB)
	if len(seen) != 2 {
		t.Errorf("the worker role must reach every tenant without a context, saw %d row(s): %v.\n"+
			"posture: %s\nCheck the *_worker_cross_tenant policies from migration 000062.", len(seen), seen, posture)
	}
}
