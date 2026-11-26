// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// build_gc_wrapper.go — hybrid wrapper for PostgresBuildGCStore.
// T2.2 (Batch B #6) of plan
// 2026-05-29-sqlc-migration-plan v1.3.
//
// DeleteOldWebhookDeliveries dispatches through SqlcBridge to the
// sqlc-generated query.
//
// DeleteOldBuilds is kept as raw SQL via DBTX.Exec because sqlc
// 1.31.1 cannot parse the ranked-subquery shape (`column rn does
// not exist` errors across multiple CTE / subquery refactors).
// Per ADR-1 D5 of docs/decisions.md, this
// query is a candidate for the raw/ subpackage escape hatch which
// is the explicit purpose of T4.1 — wrapping the handwritten SQL
// behind a typed boundary so the sqlc gate's invariants
// (TenantOwnedTables, parameterization) can still be enforced
// against it.

import (
	"context"
	"fmt"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
)

// NOTE on raw subpackage seam (T4.1):
// The window-function-based ranked-DELETE used in DeleteOldBuilds
// below is the canonical T4.1 raw/ candidate. The corresponding
// implementation lives in api/internal/db/raw/delete_old_builds.go
// behind a raw.DBTX interface. To avoid an import cycle (raw cannot
// import db, but db could import raw), this wrapper keeps the SQL
// inline; callers outside the db package (e.g., services/scheduler
// paths) MAY invoke raw.DeleteOldBuilds(ctx, dbAdapter, ...) directly,
// which exercises the same SQL with the gate-protected boundary.
// See raw/README.md for the scope + sensitive-tables exclusion.

// PostgresBuildGCStore implements build.BuildGCStore for garbage
// collection.
//
// T2.2 Batch B #6: DB field type preserved (db.DBTX); methods
// dispatch through SqlcBridge for the sqlc-migrated query
// (DeleteOldWebhookDeliveries) and continue to use raw DBTX.Exec
// for DeleteOldBuilds (T4.1 raw/ candidate).
type PostgresBuildGCStore struct {
	DB db.DBTX
}

// DeleteOldBuilds removes builds older than maxAge, keeping at least
// retainPerProject most recent builds per (project_id, environment).
//
// Implementation: raw SQL via DBTX.Exec (NOT sqlc). The DELETE WHERE
// id IN (SELECT … FROM (SELECT ROW_NUMBER() OVER PARTITION …))
// shape uses a window-function-based ranked subquery that sqlc
// 1.31.1 cannot parse — multiple CTE / scalar-subquery / aliased
// rewrites all hit `column rn does not exist` errors. Per ADR-1 D5,
// this query is deferred to the T4.1 raw/ subpackage adoption.
//
// Builds in terminal state (SUCCEEDED, FAILED, CANCELLED) older
// than cutoff are removed, EXCEPT for the top N most recent per
// (tenant_id, project_id, environment) — guaranteeing the audit/history of
// the latest deploys per environment is preserved even at high
// build churn.
func (s *PostgresBuildGCStore) DeleteOldBuilds(ctx context.Context, maxAge time.Duration, retainPerProject int) (int64, error) {
	cutoff := time.Now().UTC().Add(-maxAge)

	// Delete builds that are:
	// 1. In terminal state (SUCCEEDED, FAILED, CANCELLED)
	// 2. Older than cutoff
	// 3. NOT in the top N most recent per (tenant, project, environment).
	//    builds.project_id is the project NAME, unique only per tenant, and
	//    this runs on the cross-tenant worker pool: without tenant_id in the
	//    partition, two tenants with a project of the same name share one
	//    retention budget and one deletes the other's retained history.
	result, err := s.DB.Exec(ctx, `
		DELETE FROM builds
		WHERE id IN (
			SELECT b.id FROM builds b
			WHERE b.status IN ('SUCCEEDED', 'FAILED', 'CANCELLED')
			  AND b.created_at < $1
			  AND b.id NOT IN (
				SELECT id FROM (
					SELECT id, ROW_NUMBER() OVER (
						PARTITION BY tenant_id, project_id, environment
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

// (DeleteOldEvents removed in dogfood-2026-05-08-fixes T1.1 / ADR D2:
//  the `build_events` table never existed in any migration. The GC
//  scheduler logged `ERROR: relation "build_events" does not exist`
//  every tick. SSE retention is handled by the sse_events path.)

// DeleteOldWebhookDeliveries removes webhook delivery dedup records
// older than maxAge. sqlc-backed via SqlcBridge dispatch.
func (s *PostgresBuildGCStore) DeleteOldWebhookDeliveries(ctx context.Context, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-maxAge)

	gdb := db.Generated(s.DB)
	q := generated.New()
	n, err := q.DeleteOldWebhookDeliveries(ctx, gdb, pgtype.Timestamptz{Time: cutoff, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("build gc: delete old webhook deliveries: %w", err)
	}
	return n, nil
}
