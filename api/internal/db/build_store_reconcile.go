// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"
)

// FindActiveBuilds returns all builds in QUEUED, RUNNING, or RELEASING state
// belonging to the given cell. Roadmap 03 adds the cell_id filter so that in
// a multi-cell deployment, each cell's reconciler only sees its own work.
// Pass CellDefault ("cell-default") for single-cell / backward-compatible
// behavior — existing data and any rows inserted without an explicit cell_id
// default to 'cell-default' via migration 000029.
func (s *PostgresBuildStore) FindActiveBuilds(ctx context.Context, cellID string) ([]Build, error) {
	if cellID == "" {
		cellID = CellDefault
	}
	rows, err := s.db.Query(ctx,
		// RELEASING included per MEET-2026-03-26-008: builds with completed images
		// that need release activation (Phase 2) to be retried after crash/failure.
		// cell_id filter uses idx_builds_cell_status_created (migration 000029).
		`SELECT id, tenant_id, project_id, environment, status, COALESCE(commit_sha, ''),
		        correlation_id, created_by, created_at, started_at, finished_at,
		        COALESCE(error_message, ''), duration_seconds, COALESCE(workflow_id, '')
		 FROM builds
		 WHERE cell_id = $1
		   AND status IN ('QUEUED', 'RUNNING', 'RELEASING')
		 ORDER BY created_at ASC
		 LIMIT 100`,
		cellID,
	)
	if err != nil {
		return nil, fmt.Errorf("build store: find active builds: %w", err)
	}
	defer rows.Close()

	var builds []Build
	for rows.Next() {
		var b Build
		if err := rows.Scan(
			&b.ID, &b.TenantID, &b.ProjectID, &b.Environment, &b.Status,
			&b.CommitSHA, &b.CorrelationID, &b.CreatedBy,
			&b.CreatedAt, &b.StartedAt, &b.FinishedAt, &b.ErrorMessage,
			&b.DurationSeconds, &b.WorkflowID,
		); err != nil {
			return nil, fmt.Errorf("build store: scan active build: %w", err)
		}
		builds = append(builds, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("build store: rows error: %w", err)
	}

	if builds == nil {
		builds = []Build{}
	}
	return builds, nil
}

// CancelActiveBuilds cancels all QUEUED or RUNNING builds for a (tenant, project, env).
// Returns the number of builds cancelled and their workflow IDs (for Argo cancellation).
// This method runs inside an advisory lock transaction.
//
// NOTE: This intentionally bypasses the state machine (direct SQL UPDATE to CANCELLED)
// for batch operations during project deletion. For RUNNING builds this skips the
// CANCELLING intermediate state — Argo workflow cancellation is handled separately
// via the returned workflow IDs. Do not reuse this for single-build cancellation.
func (s *PostgresBuildStore) CancelActiveBuilds(ctx context.Context, tx DBTX, tenantID, projectID, env string) (int, []string, error) {
	executor := tx
	if executor == nil {
		executor = s.db
	}
	rows, err := executor.Query(ctx,
		`UPDATE builds SET status = 'CANCELLED', finished_at = NOW()
		 WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
		 AND status IN ('QUEUED', 'RUNNING')
		 RETURNING COALESCE(workflow_id, '')`,
		tenantID, projectID, env,
	)
	if err != nil {
		return 0, nil, fmt.Errorf("build store: cancel active builds: %w", err)
	}
	defer rows.Close()

	var workflowIDs []string
	count := 0
	for rows.Next() {
		var wfID string
		if err := rows.Scan(&wfID); err != nil {
			return 0, nil, fmt.Errorf("build store: scan workflow_id: %w", err)
		}
		count++
		if wfID != "" {
			workflowIDs = append(workflowIDs, wfID)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("build store: cancel active builds rows: %w", err)
	}
	return count, workflowIDs, nil
}
