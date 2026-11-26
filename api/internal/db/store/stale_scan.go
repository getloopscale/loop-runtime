// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"context"
	"fmt"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// StaleReleaseRow is a release that has sat in READY or PENDING past a cutoff.
// Which timeout event it deserves is the reaper's decision, not this query's.
type StaleReleaseRow struct {
	ID, Status, TenantID, ProjectID, Environment, BuildID string
}

// StaleBuildRow is a build that has sat in a timed phase past its cutoff.
type StaleBuildRow struct {
	ID, Status string
}

// StaleBuildCutoffs are the instants before which a build in each timed phase
// is stale. QUEUED is measured from created_at, the others from started_at.
type StaleBuildCutoffs struct {
	QueuedBefore, RunningBefore, ReleasingBefore, CancellingBefore time.Time
}

// PostgresStaleScanner finds rows the reapers must time out. The queries are
// scoped to one cell (roadmap 03); an empty cell means every cell — how the API
// runs its loops (#447, #450). Both are capped at 100 rows, oldest first.
type PostgresStaleScanner struct {
	DB db.DBTX
}

// StaleReleaseRows returns releases in READY created before readyBefore and in
// PENDING created before pendingBefore, in cellID or — when cellID is empty — in
// every cell, which is how the API runs its loops (#447). A cell filter uses
// idx_releases_cell_status_created (migration 000029).
func (s *PostgresStaleScanner) StaleReleaseRows(ctx context.Context, cellID string, readyBefore, pendingBefore time.Time) ([]StaleReleaseRow, error) {
	const query = `
		SELECT id, status, tenant_id, project_id, environment, build_id FROM releases
		WHERE ($3 = '' OR cell_id = $3)
		  AND ((status = 'READY' AND created_at < $1)
		    OR (status = 'PENDING' AND created_at < $2))
		ORDER BY created_at ASC
		LIMIT 100
	`
	rows, err := s.DB.Query(ctx, query, readyBefore, pendingBefore, cellID)
	if err != nil {
		return nil, fmt.Errorf("find stale releases: %w", err)
	}
	defer rows.Close()

	var result []StaleReleaseRow
	for rows.Next() {
		var r StaleReleaseRow
		if err := rows.Scan(&r.ID, &r.Status, &r.TenantID, &r.ProjectID, &r.Environment, &r.BuildID); err != nil {
			return nil, fmt.Errorf("scan stale release: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// StaleBuildRows returns builds in QUEUED, RUNNING, RELEASING or CANCELLING
// past their cutoff.
func (s *PostgresStaleScanner) StaleBuildRows(ctx context.Context, cellID string, cutoffs StaleBuildCutoffs) ([]StaleBuildRow, error) {
	const query = `
		SELECT id, status FROM builds
		WHERE ($5 = '' OR cell_id = $5)
		  AND ((status = 'QUEUED' AND created_at < $1)
		    OR (status = 'RUNNING' AND started_at < $2)
		    OR (status = 'RELEASING' AND started_at < $3)
		    OR (status = 'CANCELLING' AND started_at < $4))
		ORDER BY created_at ASC
		LIMIT 100
	`
	rows, err := s.DB.Query(ctx, query,
		cutoffs.QueuedBefore, cutoffs.RunningBefore, cutoffs.ReleasingBefore, cutoffs.CancellingBefore,
		cellID)
	if err != nil {
		return nil, fmt.Errorf("find stale builds: %w", err)
	}
	defer rows.Close()

	var result []StaleBuildRow
	for rows.Next() {
		var r StaleBuildRow
		if err := rows.Scan(&r.ID, &r.Status); err != nil {
			return nil, fmt.Errorf("scan stale build: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
