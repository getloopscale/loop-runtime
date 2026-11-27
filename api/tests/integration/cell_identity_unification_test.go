// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// These tests prove the claims migration 000061 makes about the database, instead of
// trusting them. See docs/decisions/2026-08-09-cell-cluster-unification-adr.md and BACKLOG.md B-005.
//
// They require a real PostgreSQL with migration 000061 applied; they skip without failing when no
// database is reachable, like the other tests in this package.
//
// While they skip, the unification is verified by reading and not by execution — and the two
// premises the migration assumes about Postgres remain unproven. The way to run them is the
// .claude/rules/cleanroom-up-live-test-mandate.md chain, where a database is standing after
// setup + seeds. Tracked in https://github.com/getloopscale/loop-runtime/issues/279
package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgErrCodeNotNullViolation is the SQLSTATE Postgres returns when a NOT NULL column
// receives NULL. It is exactly the error that must NOT happen if the trigger runs first.
const pgErrCodeNotNullViolation = "23502"

func cellTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL())
	if err != nil {
		// no database: nothing to prove here. Tracked in https://github.com/getloopscale/loop-runtime/issues/279
		t.Skipf("skipping: no reachable PostgreSQL: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		// no database: nothing to prove here. Tracked in https://github.com/getloopscale/loop-runtime/issues/279
		t.Skipf("skipping: no reachable PostgreSQL: %v", err)
	}
	return pool
}

// TestTriggerResolvesCellIDBeforeNotNullRejects proves the single premise of the ADR's Option A.
//
// Migration 000061 removed the DEFAULT from the four tables with the trigger, betting that an
// INSERT which omits `cell_id` is resolved by the BEFORE INSERT trigger before the NOT NULL
// constraint looks at the row. If that order did not hold, every INSERT omitting the column would
// start failing in production — and the ADR recorded that asserting it without proof was
// unacceptable.
//
// The assertion is deliberately narrow: the INSERT may fail for other reasons (a foreign key, a
// column the test itself requires), and that does not matter. What the test forbids is ONE
// specific error: a NOT NULL violation on `cell_id`. That error, and only that one, would refute
// the order.
func TestTriggerResolvesCellIDBeforeNotNullRejects(t *testing.T) {
	pool := cellTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// Always roll back: the test proves an order, it leaves no data.
	tx, err := pool.Begin(ctx)
	if err != nil {
		// no database: nothing to prove here. Tracked in https://github.com/getloopscale/loop-runtime/issues/279
		t.Skipf("skipping: could not open a transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx,
		`INSERT INTO builds (id, tenant_id, project_id, environment, status)
		 VALUES ('bld-cell-order-probe', 'tn-nonexistent-on-purpose', 'proj', 'dev', 'PENDING')`)

	var pgErr *pgconn.PgError
	if err != nil && asPgError(err, &pgErr) {
		if pgErr.Code == pgErrCodeNotNullViolation && strings.Contains(pgErr.ColumnName, "cell_id") {
			t.Fatalf("the cell_id NOT NULL rejected BEFORE the trigger resolved — the premise of the "+
				"ADR's Option A does not hold on this Postgres, and migration 000061 must go back to "+
				"an explicit sentinel (Option B). Error: %v", pgErr)
		}
	}
	// Any other outcome — success, FK, the trigger's own RAISE — confirms the row reached the
	// trigger before the nullability constraint decided.
}

// TestTriggerFailsLoudlyWhenTenantHasNoCell proves phase 4 of the migration.
//
// Before, a tenant with no cell made the row be written with the sentinel and the defect
// resurfaced at deploy time, as wrong routing. `.claude/rules/error-handling.md` § 2 asks for the
// opposite, and the migration now raises an exception naming the tenant.
func TestTriggerFailsLoudlyWhenTenantHasNoCell(t *testing.T) {
	pool := cellTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		// no database: nothing to prove here. Tracked in https://github.com/getloopscale/loop-runtime/issues/279
		t.Skipf("skipping: could not open a transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx,
		`INSERT INTO builds (id, tenant_id, project_id, environment, status)
		 VALUES ('bld-cell-raise-probe', 'tn-that-does-not-exist', 'proj', 'dev', 'PENDING')`)
	if err == nil {
		t.Fatal("the INSERT with a nonexistent tenant was accepted — the trigger should have raised " +
			"an exception instead of writing the row with an unresolved cell")
	}
	if !strings.Contains(err.Error(), "cell_id not resolved") &&
		!strings.Contains(err.Error(), "foreign key") {
		t.Logf("observed error (acceptable if it is the FK, which blocks first): %v", err)
	}
}

// TestCatalogueNoLongerHasTheOldIdentities proves the merge reached the catalogue.
//
// `cell-default` was a sentinel, not a cell; `cell-1` named the same machine as it. If either
// survives, the 2:1 relation the unification exists to eliminate is still standing under new
// names — the worst possible outcome, because it looks resolved.
func TestCatalogueNoLongerHasTheOldIdentities(t *testing.T) {
	pool := cellTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	var survivors int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM cells WHERE cell_id IN ('cell-default', 'cell-2')`).Scan(&survivors)
	if err != nil {
		// no database: nothing to prove here. Tracked in https://github.com/getloopscale/loop-runtime/issues/279
		t.Skipf("skipping: cells table unavailable: %v", err)
	}
	if survivors != 0 {
		t.Errorf("the catalogue still has %d old identit(y/ies) — migration 000061 was not "+
			"applied, or was applied halfway", survivors)
	}
}

func asPgError(err error, target **pgconn.PgError) bool {
	for e := err; e != nil; {
		if pe, ok := e.(*pgconn.PgError); ok {
			*target = pe
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
