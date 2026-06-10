// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package raw

// delete_old_builds.go — first raw query that lands in the escape
// hatch. T4.1 of plan docs/plans/2026-05-29-sqlc-migration-plan.md
// v1.3.
//
// sqlc failure mode (sqlc 1.31.1):
//
//   `column rn does not exist`
//
// across multiple CTE / scalar-subquery / aliased rewrites of the
// ranked subquery shape. sqlc's planner walks the inner SELECT and
// the outer DELETE; the `rn` column alias declared on the inner
// SELECT is not visible to the outer planner pass.
//
// Workaround: implement via db.DBTX.Exec (same testable abstraction
// used by the rest of the wrappers). The raw SQL preserves the
// 3-predicate WHERE — terminal status + cutoff age + NOT IN top-N
// most-recent per (project_id, environment) — verbatim from the
// legacy build_gc_store.go path.
//
// Table touched: `builds` (NOT sensitive per sensitive_tables.go).

import (
	"context"
	"fmt"
	"time"
)

// ExecResult mirrors db.ExecResult to avoid a cyclic import. The
// shape is intentionally minimal — raw queries only need
// rows-affected from the Exec primitive.
type ExecResult struct {
	RowsAffected int64
}

// DBTX is the minimal contract raw queries depend on. The
// production *db.PgxAdapter / *db.PgxTxAdapter implements this
// shape — Go's structural typing makes the bridge automatic with
// no adapter code.
//
// We mirror the interface here instead of importing api/internal/db
// to avoid the cyclic dependency that arises when build_gc_wrapper.go
// in the db package imports raw.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (ExecResult, error)
}

// DeleteOldBuilds removes builds in terminal state older than
// maxAge, keeping at least retainPerProject most recent builds per
// (project_id, environment).
//
// Caller passes a raw.DBTX (the production *db.PgxAdapter /
// *db.PgxTxAdapter satisfy the interface via Go's structural typing
// — no adapter needed).
//
// Build GC scheduler call site lives in services.BuildGCSchedulerLoop;
// the wrapper PostgresBuildGCStore.DeleteOldBuilds is a thin
// passthrough to this function so the GC loop remains a single
// composition seam.
func DeleteOldBuilds(ctx context.Context, dbtx DBTX, maxAge time.Duration, retainPerProject int) (int64, error) {
	cutoff := time.Now().UTC().Add(-maxAge)

	// Delete builds that are:
	// 1. In terminal state (SUCCEEDED, FAILED, CANCELLED)
	// 2. Older than cutoff
	// 3. NOT in the top N most recent per (project, environment)
	result, err := dbtx.Exec(ctx, `
		DELETE FROM builds
		WHERE id IN (
			SELECT b.id FROM builds b
			WHERE b.status IN ('SUCCEEDED', 'FAILED', 'CANCELLED')
			  AND b.created_at < $1
			  AND b.id NOT IN (
				SELECT id FROM (
					SELECT id, ROW_NUMBER() OVER (
						PARTITION BY project_id, environment
						ORDER BY created_at DESC
					) as rn
					FROM builds
				) ranked
				WHERE rn <= $2
			  )
		)
	`, cutoff, retainPerProject)
	if err != nil {
		return 0, fmt.Errorf("build gc: delete old builds: %w", err)
	}
	return result.RowsAffected, nil
}
