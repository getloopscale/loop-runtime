// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package integration tests database hardening (MEET-2026-03-31-024 Phase 1).
//
// These tests verify:
// 1. Role separation: loopzero_api cannot execute DDL
// 2. RLS isolation: tenant-A cannot see tenant-B data
// 3. RLS bypass: system queries (no tenant_id set) see all rows
// 4. Reconciliation queries work cross-tenant with RLS active
//
// These tests require a real PostgreSQL instance with migration 000009 applied.
// They skip gracefully if DATABASE_URL is not set and the default connection fails.
package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// setupHardeningTestDB creates a pool and ensures migrations are applied.
func setupHardeningTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL())
	if err != nil {
		t.Skipf("skipping: cannot connect to PostgreSQL: %v", err)
	}

	// Check if migration 000009 has been applied by checking for RLS
	var rlsEnabled bool
	err = pool.QueryRow(ctx,
		"SELECT relrowsecurity FROM pg_class WHERE relname = 'builds'").Scan(&rlsEnabled)
	if err != nil || !rlsEnabled {
		t.Skipf("skipping: migration 000009 not applied (RLS not enabled on builds)")
	}

	t.Cleanup(func() { pool.Close() })
	return pool
}

// TestRLSTenantIsolation verifies that RLS enforces tenant isolation.
// When app.tenant_id is set, only rows matching that tenant are visible.
func TestRLSTenantIsolation(t *testing.T) {
	pool := setupHardeningTestDB(t)
	ctx := context.Background()

	// Setup: create test data for two tenants
	tenantA := fmt.Sprintf("test-tenant-a-%d", time.Now().UnixNano())
	tenantB := fmt.Sprintf("test-tenant-b-%d", time.Now().UnixNano())

	// Insert builds for both tenants
	buildA := fmt.Sprintf("bld-a-%d", time.Now().UnixNano())
	buildB := fmt.Sprintf("bld-b-%d", time.Now().UnixNano())

	_, err := pool.Exec(ctx,
		`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
		 VALUES ($1, $2, 'proj-test', 'production', 'QUEUED', 'corr-1', 'test')`,
		buildA, tenantA)
	if err != nil {
		t.Fatalf("failed to insert build for tenant A: %v", err)
	}

	_, err = pool.Exec(ctx,
		`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
		 VALUES ($1, $2, 'proj-test', 'production', 'QUEUED', 'corr-2', 'test')`,
		buildB, tenantB)
	if err != nil {
		t.Fatalf("failed to insert build for tenant B: %v", err)
	}

	// Cleanup
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM builds WHERE id IN ($1, $2)", buildA, buildB)
	})

	t.Run("tenant A cannot see tenant B builds when tenant_id is set", func(t *testing.T) {
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		// Set tenant context to tenant A
		_, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantA)
		if err != nil {
			t.Fatalf("set_config: %v", err)
		}

		// Query all builds — should only see tenant A's build
		rows, err := tx.Query(ctx, "SELECT id, tenant_id FROM builds WHERE id IN ($1, $2)", buildA, buildB)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			var id, tid string
			if err := rows.Scan(&id, &tid); err != nil {
				t.Fatalf("scan: %v", err)
			}
			if tid != tenantA {
				t.Errorf("expected tenant_id=%s, got %s (RLS leak!)", tenantA, tid)
			}
			count++
		}

		if count != 1 {
			t.Errorf("expected 1 build visible to tenant A, got %d", count)
		}
	})

	t.Run("system queries (no tenant_id set) see all builds", func(t *testing.T) {
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		// Do NOT set app.tenant_id — simulates reconciliation/reaper queries
		rows, err := tx.Query(ctx, "SELECT id FROM builds WHERE id IN ($1, $2)", buildA, buildB)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatalf("scan: %v", err)
			}
			count++
		}

		if count != 2 {
			t.Errorf("expected 2 builds visible to system query (no tenant_id), got %d", count)
		}
	})
}

// TestRLSOnAllTenantTables verifies RLS is active on every tenant-scoped table.
func TestRLSOnAllTenantTables(t *testing.T) {
	pool := setupHardeningTestDB(t)
	ctx := context.Background()

	tenantTables := []string{
		"builds", "releases", "environment_targets", "secrets",
		"projects", "tenant_members", "audit_log", "idempotency_keys",
	}

	for _, table := range tenantTables {
		t.Run(fmt.Sprintf("RLS enabled on %s", table), func(t *testing.T) {
			var rlsEnabled, rlsForced bool
			err := pool.QueryRow(ctx,
				"SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE relname = $1",
				table).Scan(&rlsEnabled, &rlsForced)
			if err != nil {
				t.Fatalf("query pg_class for %s: %v", table, err)
			}
			if !rlsEnabled {
				t.Errorf("RLS not enabled on %s", table)
			}
			if !rlsForced {
				t.Errorf("RLS not forced on %s (table owner bypasses)", table)
			}
		})
	}
}

// TestCheckConstraintsOnStatus verifies CHECK constraints exist on status columns.
func TestCheckConstraintsOnStatus(t *testing.T) {
	pool := setupHardeningTestDB(t)
	ctx := context.Background()

	t.Run("builds status CHECK constraint exists", func(t *testing.T) {
		var exists bool
		err := pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conname = 'chk_builds_status')").Scan(&exists)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if !exists {
			t.Error("CHECK constraint chk_builds_status does not exist")
		}
	})

	t.Run("releases status CHECK constraint exists", func(t *testing.T) {
		var exists bool
		err := pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conname = 'chk_releases_status')").Scan(&exists)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if !exists {
			t.Error("CHECK constraint chk_releases_status does not exist")
		}
	})

	t.Run("invalid build status rejected by CHECK", func(t *testing.T) {
		_, err := pool.Exec(ctx,
			`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
			 VALUES ('bld-invalid-status', 'test', 'proj', 'prod', 'INVALID_STATUS', 'corr', 'test')`)
		if err == nil {
			// Cleanup if somehow it succeeded
			_, _ = pool.Exec(ctx, "DELETE FROM builds WHERE id = 'bld-invalid-status'")
			t.Error("expected CHECK constraint violation for invalid status, but INSERT succeeded")
		}
	})
}

// TestRolesExist verifies that the three database roles were created.
func TestRolesExist(t *testing.T) {
	pool := setupHardeningTestDB(t)
	ctx := context.Background()

	roles := []string{"loopzero_api", "loopzero_migrate", "loopzero_readonly"}
	for _, role := range roles {
		t.Run(fmt.Sprintf("role %s exists", role), func(t *testing.T) {
			var exists bool
			err := pool.QueryRow(ctx,
				"SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)", role).Scan(&exists)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			if !exists {
				t.Errorf("role %s does not exist", role)
			}
		})
	}
}

// TestLoopZeroApiCannotDDL verifies the loopzero_api role cannot execute DDL operations.
// This test requires connecting as loopzero_api via LOOPZERO_API_DATABASE_URL env var.
// Skips if the env var is not set or connection fails.
func TestLoopZeroApiCannotDDL(t *testing.T) {
	ctx := context.Background()

	// Use a dedicated env var for the loopzero_api connection string.
	// This avoids overwriting with the admin URL from DATABASE_URL.
	apiURL := os.Getenv("LOOPZERO_API_DATABASE_URL")
	if apiURL == "" {
		t.Skip("skipping: LOOPZERO_API_DATABASE_URL not set (needed to test loopzero_api role)")
	}

	connCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(connCtx, apiURL)
	if err != nil {
		t.Skipf("skipping: cannot connect as loopzero_api: %v", err)
	}
	defer pool.Close()

	t.Run("DROP TABLE is denied", func(t *testing.T) {
		_, err := pool.Exec(ctx, "DROP TABLE IF EXISTS builds")
		if err == nil {
			t.Error("expected permission denied for DROP TABLE, but it succeeded!")
		}
	})

	t.Run("ALTER TABLE is denied", func(t *testing.T) {
		_, err := pool.Exec(ctx, "ALTER TABLE builds ADD COLUMN test_col TEXT")
		if err == nil {
			_, _ = pool.Exec(ctx, "ALTER TABLE builds DROP COLUMN test_col")
			t.Error("expected permission denied for ALTER TABLE, but it succeeded!")
		}
	})

	t.Run("TRUNCATE is denied", func(t *testing.T) {
		_, err := pool.Exec(ctx, "TRUNCATE builds")
		if err == nil {
			t.Error("expected permission denied for TRUNCATE, but it succeeded!")
		}
	})

	t.Run("SELECT is allowed", func(t *testing.T) {
		_, err := pool.Exec(ctx, "SELECT 1 FROM builds LIMIT 1")
		if err != nil {
			t.Errorf("expected SELECT to be allowed, got: %v", err)
		}
	})
}

// TestAuditLogAppendOnly verifies the audit_log table rejects UPDATE and DELETE.
func TestAuditLogAppendOnly(t *testing.T) {
	pool := setupHardeningTestDB(t)
	ctx := context.Background()

	// Insert a test audit entry
	_, err := pool.Exec(ctx,
		`INSERT INTO audit_log (tenant_id, action, actor, resource_type, resource_id, correlation_id)
		 VALUES ('test-audit', 'TEST', 'test', 'test', 'test-id', 'corr-test')`)
	if err != nil {
		t.Fatalf("failed to insert audit entry: %v", err)
	}

	t.Run("UPDATE on audit_log is rejected by trigger", func(t *testing.T) {
		// The deferred trigger fires at commit — we need to commit to see the error
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}

		_, err = tx.Exec(ctx,
			"UPDATE audit_log SET action = 'TAMPERED' WHERE tenant_id = 'test-audit'")
		if err != nil {
			// Some configurations reject immediately
			_ = tx.Rollback(ctx)
			return // Expected — trigger fired
		}

		// Deferred trigger fires at commit
		err = tx.Commit(ctx)
		if err == nil {
			t.Error("expected audit_log UPDATE to be rejected by trigger, but commit succeeded")
		}
	})

	t.Run("DELETE on audit_log is rejected by trigger", func(t *testing.T) {
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}

		_, err = tx.Exec(ctx,
			"DELETE FROM audit_log WHERE tenant_id = 'test-audit'")
		if err != nil {
			_ = tx.Rollback(ctx)
			return // Expected — trigger fired
		}

		err = tx.Commit(ctx)
		if err == nil {
			t.Error("expected audit_log DELETE to be rejected by trigger, but commit succeeded")
		}
	})

	// Cleanup — need to disable trigger temporarily to clean test data
	// In production, old audit entries would be archived via partition rotation
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "ALTER TABLE audit_log DISABLE TRIGGER trg_audit_log_immutable")
		_, _ = pool.Exec(ctx, "DELETE FROM audit_log WHERE tenant_id = 'test-audit'")
		_, _ = pool.Exec(ctx, "ALTER TABLE audit_log ENABLE TRIGGER trg_audit_log_immutable")
	})
}

// TestReconciliationQueriesWithRLS verifies that cross-tenant system queries
// (FindActiveBuilds, FindPendingReleases patterns) work with RLS active.
func TestReconciliationQueriesWithRLS(t *testing.T) {
	pool := setupHardeningTestDB(t)
	ctx := context.Background()

	tenantA := fmt.Sprintf("recon-a-%d", time.Now().UnixNano())
	tenantB := fmt.Sprintf("recon-b-%d", time.Now().UnixNano())
	buildA := fmt.Sprintf("bld-recon-a-%d", time.Now().UnixNano())
	buildB := fmt.Sprintf("bld-recon-b-%d", time.Now().UnixNano())

	// Insert QUEUED builds for two different tenants
	for _, tc := range []struct {
		id, tenant string
	}{{buildA, tenantA}, {buildB, tenantB}} {
		_, err := pool.Exec(ctx,
			`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
			 VALUES ($1, $2, 'proj-recon', 'production', 'QUEUED', 'corr-recon', 'test')`,
			tc.id, tc.tenant)
		if err != nil {
			t.Fatalf("insert build %s: %v", tc.id, err)
		}
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM builds WHERE id IN ($1, $2)", buildA, buildB)
	})

	t.Run("FindActiveBuilds pattern sees both tenants without tenant_id set", func(t *testing.T) {
		// This simulates the reconciliation loop pattern:
		// SELECT * FROM builds WHERE status IN ('QUEUED', 'RUNNING')
		// No SET LOCAL app.tenant_id — system query
		rows, err := pool.Query(ctx,
			`SELECT id, tenant_id FROM builds
			 WHERE status IN ('QUEUED', 'RUNNING')
			 AND id IN ($1, $2)`,
			buildA, buildB)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer rows.Close()

		seen := map[string]bool{}
		for rows.Next() {
			var id, tid string
			if err := rows.Scan(&id, &tid); err != nil {
				t.Fatalf("scan: %v", err)
			}
			seen[tid] = true
		}

		if !seen[tenantA] || !seen[tenantB] {
			t.Errorf("reconciliation query must see both tenants, saw: %v", seen)
		}
	})
}

// TestIndexesExist verifies that migration 000009 indexes were created.
func TestIndexesExist(t *testing.T) {
	pool := setupHardeningTestDB(t)
	ctx := context.Background()

	indexes := []string{
		"idx_releases_build_id",
		"idx_releases_pending",
	}

	for _, idx := range indexes {
		t.Run(fmt.Sprintf("index %s exists", idx), func(t *testing.T) {
			var exists bool
			err := pool.QueryRow(ctx,
				"SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE indexname = $1)", idx).Scan(&exists)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			if !exists {
				t.Errorf("index %s does not exist", idx)
			}
		})
	}

	t.Run("redundant idempotency index was dropped", func(t *testing.T) {
		var exists bool
		err := pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE indexname = 'idempotency_keys_key_tenant_idx')").Scan(&exists)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if exists {
			t.Error("redundant index idempotency_keys_key_tenant_idx should have been dropped")
		}
	})
}
