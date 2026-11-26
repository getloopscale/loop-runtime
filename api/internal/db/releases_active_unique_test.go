// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

// releases_active_unique_test.go — T5.1 of plan 2026-06-01-p3-infra-
// observability-test-debt-plan.md v1.1 — regression tests for the
// partial unique index `idx_releases_active_unique` added by
// migration 000022_releases_active_unique_index.up.sql.
//
// The migration enforces the at-most-one-ACTIVE-release-per-env
// invariant at the DB layer (CREATE UNIQUE INDEX ... WHERE
// status='ACTIVE'). Pre-migration the invariant was app-layer-only
// (state machine transitions); a race in build_orchestrator or a
// transition-handler bug could silently leave two ACTIVE rows for
// the same (tenant, project, env).
//
// These tests prove THREE assertions per the plan's RED-first cycle:
//   1. Second INSERT of an ACTIVE release for the same env fails
//      with SQLSTATE 23505 (TestReleases_PartialUniqueIndex_
//      RejectsSecondActive).
//   2. ROLLED_BACK→ACTIVE swap succeeds — the partial-where clause
//      does NOT block historic ROLLED_BACK rows from coexisting
//      with the new ACTIVE row (TestReleases_RolledBackThenNew
//      Active_Succeeds).
//   3. Pre-flight detection query identifies existing violations
//      BEFORE the migration applies (TestReleases_PreFlightCheck_
//      DetectsExistingViolations).
//
// Per D6 ADR of the plan, this is an integration test against a real
// PostgreSQL instance with migration 000022 applied. Skips gracefully
// when PG is unreachable OR the index has not yet been created.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// releasesUniqueDBURL mirrors secretL19DBURL. Kept inline (NOT in a
// shared helper) so the test file is self-contained — explicit per
// plan T5.1 acceptance criteria.
// releasesUniqueDBURL is the cross-tenant worker role when LOOPZERO_WORKER_DATABASE_URL is
// set: these fixtures write rows of arbitrary tenants with no tenant scope, which under
// fail-closed RLS only the *_worker_cross_tenant policies admit — the owner lost its
// maintenance policies in 000064 (#387).
func releasesUniqueDBURL() string {
	if u := os.Getenv("LOOPZERO_WORKER_DATABASE_URL"); u != "" {
		return u
	}
	return secretL19DBURL()
}

func releasesUniquePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return releasesPoolAt(t, releasesUniqueDBURL())
}

func releasesPoolAt(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Skipf("T5.1 test skipped: cannot create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("T5.1 test skipped: cannot ping PG: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// requireReleasesUniqueIndex skips when migration 000022 has not been
// applied (the partial unique index doesn't exist yet).
func requireReleasesUniqueIndex(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE schemaname = 'public'
			  AND indexname = 'idx_releases_active_unique'
		)`).Scan(&exists)
	if err != nil {
		t.Skipf("T5.1 test skipped: cannot query pg_indexes: %v", err)
	}
	if !exists {
		t.Skipf("T5.1 test skipped: idx_releases_active_unique missing (run migration 000022)")
	}
}

// seedActiveRelease inserts the minimal (build → release) chain
// needed to assert the partial-unique-index contract. Returns the
// release ID + tenant ID so tests can clean up. Status defaults to
// 'ACTIVE'.
func seedActiveRelease(t *testing.T, pool *pgxpool.Pool, tenantID, projectID, env, status string) (releaseID string) {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d-%s-%s", time.Now().UnixNano(), env, status)
	buildID := "bld-uniq-" + suffix
	releaseID = "rel-uniq-" + suffix
	corrID := "corr-uniq-" + suffix

	seedTenantRow(t, pool, tenantID)
	_, err := pool.Exec(ctx,
		`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
		 VALUES ($1, $2, $3, $4, 'SUCCEEDED', $5, 'system:test')`,
		buildID, tenantID, projectID, env, corrID)
	if err != nil {
		t.Fatalf("seed build: %v", err)
	}

	seedTenantRow(t, pool, tenantID)
	seedTenantRow(t, pool, tenantID)
	seedTenantRow(t, pool, tenantID)
	seedTenantRow(t, pool, tenantID)
	seedTenantRow(t, pool, tenantID)
	_, err = pool.Exec(ctx,
		`INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'system:test')`,
		releaseID, buildID, tenantID, projectID, env, status, corrID)
	if err != nil {
		t.Fatalf("seed release status=%q: %v", status, err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM releases WHERE id = $1", releaseID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM builds WHERE id = $1", buildID)
	})
	return releaseID
}

// TestReleases_PartialUniqueIndex_RejectsSecondActive (T5.1 RED #1):
// inserting a SECOND ACTIVE release for the same (tenant, project,
// env) fails with PG SQLSTATE 23505 (unique violation) referencing
// idx_releases_active_unique. The index name in the error is the
// contract callers depend on for the HTTP 409 CONCURRENT_ACTIVATION
// mapping.
func TestReleases_PartialUniqueIndex_RejectsSecondActive(t *testing.T) {
	pool := releasesUniquePool(t)
	requireReleasesUniqueIndex(t, pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantID := "tn-uniq-rej-" + suffix
	projectID := "proj-uniq-rej"
	env := "production"

	// First ACTIVE row: succeeds.
	_ = seedActiveRelease(t, pool, tenantID, projectID, env, "ACTIVE")

	// Second ACTIVE row for the same (tenant, project, env): must
	// fail with SQLSTATE 23505 + reference our index name.
	_, err := pool.Exec(context.Background(),
		`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
		 VALUES ('bld-uniq-2nd-`+suffix+`', $1, $2, $3, 'SUCCEEDED', 'corr-uniq-2nd-`+suffix+`', 'system:test')`,
		tenantID, projectID, env)
	if err != nil {
		t.Fatalf("seed second build: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM builds WHERE id = $1", "bld-uniq-2nd-"+suffix)
	})

	_, insertErr := pool.Exec(context.Background(),
		`INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by)
		 VALUES ($1, $2, $3, $4, $5, 'ACTIVE', $6, 'system:test')`,
		"rel-uniq-2nd-"+suffix, "bld-uniq-2nd-"+suffix, tenantID, projectID, env, "corr-uniq-2nd-"+suffix)
	if insertErr == nil {
		// Cleanup the unexpected row before failing.
		_, _ = pool.Exec(context.Background(), "DELETE FROM releases WHERE id = $1", "rel-uniq-2nd-"+suffix)
		t.Fatal("second ACTIVE INSERT must FAIL — partial unique index regression")
	}

	var pgErr *pgconn.PgError
	if !errors.As(insertErr, &pgErr) {
		t.Fatalf("expected *pgconn.PgError, got %T: %v", insertErr, insertErr)
	}
	if pgErr.SQLState() != "23505" {
		t.Errorf("expected SQLSTATE 23505 (unique violation), got %q: %v", pgErr.SQLState(), pgErr.Message)
	}
	if !strings.Contains(pgErr.Message, "idx_releases_active_unique") {
		t.Errorf("expected error to reference idx_releases_active_unique, got: %q", pgErr.Message)
	}
}

// TestReleases_RolledBackThenNewActive_Succeeds (T5.1 RED #2): a
// ROLLED_BACK release coexists with a new ACTIVE release for the
// same env because the partial-where clause `WHERE status='ACTIVE'`
// excludes ROLLED_BACK rows from the uniqueness scope. Locks the
// edge case from D4 deep-dive: the rollback state machine creates
// ROLLED_BACK + new ACTIVE atomically and must not deadlock the
// invariant.
func TestReleases_RolledBackThenNewActive_Succeeds(t *testing.T) {
	pool := releasesUniquePool(t)
	requireReleasesUniqueIndex(t, pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantID := "tn-uniq-rb-" + suffix
	projectID := "proj-uniq-rb"
	env := "production"

	// First release: now ROLLED_BACK (no longer ACTIVE).
	_ = seedActiveRelease(t, pool, tenantID, projectID, env, "ROLLED_BACK")

	// New ACTIVE release for the same env: MUST succeed because the
	// partial index excludes the ROLLED_BACK row.
	_, err := pool.Exec(context.Background(),
		`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
		 VALUES ('bld-uniq-rb-2-`+suffix+`', $1, $2, $3, 'SUCCEEDED', 'corr-uniq-rb-2-`+suffix+`', 'system:test')`,
		tenantID, projectID, env)
	if err != nil {
		t.Fatalf("seed second build: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM builds WHERE id = $1", "bld-uniq-rb-2-"+suffix)
	})

	relID := "rel-uniq-rb-2-" + suffix
	_, insertErr := pool.Exec(context.Background(),
		`INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by)
		 VALUES ($1, $2, $3, $4, $5, 'ACTIVE', $6, 'system:test')`,
		relID, "bld-uniq-rb-2-"+suffix, tenantID, projectID, env, "corr-uniq-rb-2-"+suffix)
	if insertErr != nil {
		t.Fatalf("new ACTIVE alongside ROLLED_BACK MUST succeed, got: %v", insertErr)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM releases WHERE id = $1", relID)
	})

	// Confirm BOTH rows exist with the expected statuses.
	var counts struct {
		active     int
		rolledBack int
	}
	err = pool.QueryRow(context.Background(),
		`SELECT
			COUNT(*) FILTER (WHERE status='ACTIVE'),
			COUNT(*) FILTER (WHERE status='ROLLED_BACK')
		 FROM releases
		 WHERE tenant_id = $1 AND project_id = $2 AND environment = $3`,
		tenantID, projectID, env).Scan(&counts.active, &counts.rolledBack)
	if err != nil {
		t.Fatalf("COUNT: %v", err)
	}
	if counts.active != 1 {
		t.Errorf("expected 1 ACTIVE row after rollback, got %d", counts.active)
	}
	if counts.rolledBack != 1 {
		t.Errorf("expected 1 ROLLED_BACK row after rollback, got %d", counts.rolledBack)
	}
}

// TestReleases_PreFlightCheck_DetectsExistingViolations (T5.1 RED #3):
// the pre-flight detection query (operator runbook) identifies
// existing ACTIVE-duplicates BEFORE the migration applies. The query
// pattern is the same one operators paste from the .up.sql header
// comment. This test injects two ACTIVE rows by temporarily DROPping
// the index (the only path that allows the dup), runs the pre-flight,
// and asserts the query returns the violating tuple.
//
// CRITICAL: this test only runs when index exists THEN drops it
// inside the test scope and re-creates at cleanup; failure mid-test
// leaves the DB without the index. Subsequent CI runs of the OTHER
// tests would skip (requireReleasesUniqueIndex). Acceptable trade-off
// for the high-signal regression: pre-flight contract is what
// operators rely on to avoid migration failures in production.
func TestReleases_PreFlightCheck_DetectsExistingViolations(t *testing.T) {
	// Dropping the index is DDL (the owner's), and the seed rows need a role RLS
	// admits unscoped; only a superuser is both, so the test runs on the audit
	// superuser the integration job already provides.
	superURL := os.Getenv("LOOPZERO_RLS_AUDIT_SUPERUSER_URL")
	if superURL == "" {
		t.Skip("needs LOOPZERO_RLS_AUDIT_SUPERUSER_URL to drop the index and seed rows under RLS — https://github.com/getloopscale/loop-runtime/issues/387")
	}
	pool := releasesPoolAt(t, superURL)
	requireReleasesUniqueIndex(t, pool)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantID := "tn-preflight-" + suffix
	projectID := "proj-preflight"
	env := "production"

	// Temporarily drop the index so we can simulate the pre-migration
	// "two ACTIVE rows" data corruption case. Wrap in a transaction
	// + always-rollback so the DB returns to clean state regardless
	// of test outcome (no orphan rows, no missing index).
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() {
		// Always rollback — leaves the index in place and discards
		// the seed inserts; ensures the OTHER two T5.1 tests can
		// still run in any order.
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, "DROP INDEX idx_releases_active_unique"); err != nil {
		t.Fatalf("DROP INDEX in tx: %v", err)
	}

	// Insert TWO ACTIVE rows for the same (tenant, project, env).
	for i := 1; i <= 2; i++ {
		buildID := fmt.Sprintf("bld-pf-%s-%d", suffix, i)
		relID := fmt.Sprintf("rel-pf-%s-%d", suffix, i)
		corrID := fmt.Sprintf("corr-pf-%s-%d", suffix, i)
		seedTenantRow(t, pool, tenantID)
		if _, err := tx.Exec(ctx,
			`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
			 VALUES ($1, $2, $3, $4, 'SUCCEEDED', $5, 'system:test')`,
			buildID, tenantID, projectID, env, corrID); err != nil {
			t.Fatalf("seed build %d: %v", i, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by)
			 VALUES ($1, $2, $3, $4, $5, 'ACTIVE', $6, 'system:test')`,
			relID, buildID, tenantID, projectID, env, corrID); err != nil {
			t.Fatalf("seed release %d: %v", i, err)
		}
	}

	// The pre-flight query from the migration .up.sql header comment.
	// Restricted to OUR tenant suffix to avoid noise from any other
	// test data in the shared DB.
	rows, err := tx.Query(ctx,
		`SELECT tenant_id, project_id, environment, COUNT(*) AS n
		 FROM releases
		 WHERE status = 'ACTIVE' AND tenant_id = $1
		 GROUP BY 1, 2, 3
		 HAVING COUNT(*) > 1`,
		tenantID)
	if err != nil {
		t.Fatalf("pre-flight query: %v", err)
	}
	defer rows.Close()

	var violations []struct {
		Tenant, Project, Env string
		N                    int
	}
	for rows.Next() {
		var v struct {
			Tenant, Project, Env string
			N                    int
		}
		if err := rows.Scan(&v.Tenant, &v.Project, &v.Env, &v.N); err != nil {
			t.Fatalf("scan: %v", err)
		}
		violations = append(violations, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation row, got %d: %+v", len(violations), violations)
	}
	if violations[0].Tenant != tenantID || violations[0].Project != projectID || violations[0].Env != env {
		t.Errorf("violation tuple drift: %+v", violations[0])
	}
	if violations[0].N != 2 {
		t.Errorf("expected count=2, got %d", violations[0].N)
	}
	// tx rollback in defer restores the index and discards seed rows.
}

// seedTenantRow creates the user/account/tenant a `builds` row needs.
//
// Migration 000061 added a trigger that resolves `builds.cell_id` from
// `tenants.cell_id` and raises when the tenant is absent. Fixtures written
// before it inserted a build for a tenant that never existed; they now fail
// with the trigger's RAISE message, which says the tenant does not exist or has
// no cell. Seeding the parents is what the trigger asks for — the honest fix
// rather than disabling it.
//
// It takes a pool or a transaction, so a fixture can be written by the role a
// transaction switched to.
func seedTenantRow(t *testing.T, pool fixtureExecer, tenantID string) {
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

// fixtureExecer is what a fixture writes through: a pool or a transaction.
type fixtureExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}
