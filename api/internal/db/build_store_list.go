// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"
)

// ListByProject returns builds for a project, ordered by created_at DESC.
func (s *PostgresBuildStore) ListByProject(ctx context.Context, tenantID, projectID string, limit, offset int) ([]Build, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, tenant_id, project_id, environment, status, COALESCE(commit_sha, ''),
		        correlation_id, created_by, created_at, started_at, finished_at,
		        COALESCE(error_message, ''), duration_seconds, COALESCE(workflow_id, '')
		 FROM builds
		 WHERE tenant_id = $1 AND project_id = $2
		 ORDER BY created_at DESC
		 LIMIT $3 OFFSET $4`,
		tenantID, projectID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("build store: list query failed: %w", err)
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
			return nil, fmt.Errorf("build store: scan failed: %w", err)
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

// ListByProjectAndEnv returns builds for a project, optionally filtered by environment.
// Returns (builds, totalCount, error). If env is empty, returns builds for all environments.
func (s *PostgresBuildStore) ListByProjectAndEnv(ctx context.Context, tenantID, projectID, env string, limit, offset int) ([]Build, int, error) {
	// Clamp limit to 1-50
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var rows Rows
	var err error
	var total int

	if env != "" {
		rows, err = s.db.Query(ctx,
			`SELECT id, tenant_id, project_id, environment, status, COALESCE(commit_sha, ''),
			        correlation_id, created_by, created_at, started_at, finished_at,
			        COALESCE(error_message, ''), duration_seconds, COALESCE(workflow_id, '')
			 FROM builds
			 WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
			 ORDER BY created_at DESC
			 LIMIT $4 OFFSET $5`,
			tenantID, projectID, env, limit, offset,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("build store: list by project and env query failed: %w", err)
		}

		err = s.db.QueryRow(ctx,
			`SELECT COUNT(*) FROM builds WHERE tenant_id = $1 AND project_id = $2 AND environment = $3`,
			tenantID, projectID, env,
		).Scan(&total)
	} else {
		rows, err = s.db.Query(ctx,
			`SELECT id, tenant_id, project_id, environment, status, COALESCE(commit_sha, ''),
			        correlation_id, created_by, created_at, started_at, finished_at,
			        COALESCE(error_message, ''), duration_seconds, COALESCE(workflow_id, '')
			 FROM builds
			 WHERE tenant_id = $1 AND project_id = $2
			 ORDER BY created_at DESC
			 LIMIT $3 OFFSET $4`,
			tenantID, projectID, limit, offset,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("build store: list by project query failed: %w", err)
		}

		err = s.db.QueryRow(ctx,
			`SELECT COUNT(*) FROM builds WHERE tenant_id = $1 AND project_id = $2`,
			tenantID, projectID,
		).Scan(&total)
	}

	if err != nil {
		return nil, 0, fmt.Errorf("build store: count query failed: %w", err)
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
			return nil, 0, fmt.Errorf("build store: scan failed: %w", err)
		}
		builds = append(builds, b)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("build store: rows error: %w", err)
	}

	if builds == nil {
		builds = []Build{}
	}
	return builds, total, nil
}
