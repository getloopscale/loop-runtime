// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"
	"time"
)

// EnvironmentTarget represents a row in the environment_targets table.
// It tracks which release is desired/active for each (tenant, project, environment).
// See spec §9 — environment_targets table, §3 — Source of Truth.
type EnvironmentTarget struct {
	TenantID         string    `json:"tenant_id"`
	ProjectID        string    `json:"project_id"`
	Environment      string    `json:"environment"`
	DesiredReleaseID string    `json:"desired_release_id"`
	ActiveReleaseID  *string   `json:"active_release_id,omitempty"`
	UpdatedBy        string    `json:"updated_by"`
	CorrelationID    string    `json:"correlation_id"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// EnvironmentTargetStore defines the interface for environment target operations.
// All tx-accepting methods (SetDesired, SetActive, DeleteTarget) run inside an existing
// transaction from withEnvironmentLock — they NEVER start their own.
type EnvironmentTargetStore interface {
	SetDesired(ctx context.Context, tx DBTX, tenantID, projectID, env, releaseID, updatedBy, correlationID string) error
	SetActive(ctx context.Context, tx DBTX, tenantID, projectID, env, releaseID string) error
	Get(ctx context.Context, tenantID, projectID, env string) (*EnvironmentTarget, error)
	ListByProject(ctx context.Context, tenantID, projectID string) ([]EnvironmentTarget, error)
	DeleteTarget(ctx context.Context, tx DBTX, tenantID, projectID, env string) error
}

// PostgresEnvironmentTargetStore implements EnvironmentTargetStore using PostgreSQL.
// It receives an ExecQueryExecutor for writes (typically a transaction) and a
// QueryExecutor for reads (typically the pool).
type PostgresEnvironmentTargetStore struct {
	Exec  ExecQueryExecutor
	Query QueryExecutor
}

// NewPostgresEnvironmentTargetStore creates a new PostgresEnvironmentTargetStore.
// exec is used for SetDesired/SetActive (runs inside the lock transaction).
// query is used for Get (can read outside a transaction).
func NewPostgresEnvironmentTargetStore(exec ExecQueryExecutor, query QueryExecutor) *PostgresEnvironmentTargetStore {
	return &PostgresEnvironmentTargetStore{
		Exec:  exec,
		Query: query,
	}
}

// SetDesired performs an UPSERT on environment_targets, setting the desired_release_id.
// On first call for a (tenant, project, environment), it inserts a new row.
// On subsequent calls, it updates desired_release_id, updated_by, and correlation_id.
// This method runs inside an existing transaction (from withEnvironmentLock).
func (s *PostgresEnvironmentTargetStore) SetDesired(ctx context.Context, tx DBTX, tenantID, projectID, env, releaseID, updatedBy, correlationID string) error {
	var executor ExecQueryExecutor
	if tx != nil {
		executor = tx
	} else {
		executor = s.Exec
	}

	const query = `INSERT INTO environment_targets (tenant_id, project_id, environment, desired_release_id, updated_by, correlation_id)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (tenant_id, project_id, environment)
DO UPDATE SET desired_release_id = $7, updated_by = $8, correlation_id = $9, updated_at = now()`

	_, err := executor.Exec(ctx, query, tenantID, projectID, env, releaseID, updatedBy, correlationID, releaseID, updatedBy, correlationID)
	if err != nil {
		return fmt.Errorf("environment target store: set desired failed: %w", err)
	}
	return nil
}

// SetActive updates the active_release_id for a (tenant, project, environment).
// This is called after ArgoCD confirms sync — never before.
// This method runs inside an existing transaction (from withEnvironmentLock).
func (s *PostgresEnvironmentTargetStore) SetActive(ctx context.Context, tx DBTX, tenantID, projectID, env, releaseID string) error {
	var executor ExecQueryExecutor
	if tx != nil {
		executor = tx
	} else {
		executor = s.Exec
	}

	const query = `UPDATE environment_targets SET active_release_id = $1, updated_at = now()
WHERE tenant_id = $2 AND project_id = $3 AND environment = $4`

	_, err := executor.Exec(ctx, query, releaseID, tenantID, projectID, env)
	if err != nil {
		return fmt.Errorf("environment target store: set active failed: %w", err)
	}
	return nil
}

// Get retrieves the environment target for a (tenant, project, environment).
// Returns nil when not found (not an error).
func (s *PostgresEnvironmentTargetStore) Get(ctx context.Context, tenantID, projectID, env string) (*EnvironmentTarget, error) {
	const q = `SELECT tenant_id, project_id, environment, desired_release_id, active_release_id, updated_by, correlation_id, updated_at
FROM environment_targets
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3`

	rows, err := s.Query.Query(ctx, q, tenantID, projectID, env)
	if err != nil {
		return nil, fmt.Errorf("environment target store: get failed: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("environment target store: rows iteration failed: %w", err)
		}
		return nil, nil
	}

	var target EnvironmentTarget
	if err := rows.Scan(
		&target.TenantID,
		&target.ProjectID,
		&target.Environment,
		&target.DesiredReleaseID,
		&target.ActiveReleaseID,
		&target.UpdatedBy,
		&target.CorrelationID,
		&target.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("environment target store: scan failed: %w", err)
	}

	return &target, nil
}

// ListByProject returns all environment targets for a (tenant, project).
func (s *PostgresEnvironmentTargetStore) ListByProject(ctx context.Context, tenantID, projectID string) ([]EnvironmentTarget, error) {
	rows, err := s.Query.Query(ctx,
		`SELECT tenant_id, project_id, environment, desired_release_id, active_release_id, updated_by, correlation_id, updated_at
FROM environment_targets
WHERE tenant_id = $1 AND project_id = $2`,
		tenantID, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("environment target store: list by project: %w", err)
	}
	defer rows.Close()

	var targets []EnvironmentTarget
	for rows.Next() {
		var t EnvironmentTarget
		if err := rows.Scan(
			&t.TenantID, &t.ProjectID, &t.Environment,
			&t.DesiredReleaseID, &t.ActiveReleaseID,
			&t.UpdatedBy, &t.CorrelationID, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("environment target store: list by project scan: %w", err)
		}
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("environment target store: list by project rows error: %w", err)
	}
	if targets == nil {
		targets = []EnvironmentTarget{}
	}
	return targets, nil
}

// DeleteTarget removes an environment target within a transaction.
func (s *PostgresEnvironmentTargetStore) DeleteTarget(ctx context.Context, tx DBTX, tenantID, projectID, env string) error {
	var executor ExecQueryExecutor
	if tx != nil {
		executor = tx
	} else {
		executor = s.Exec
	}

	_, err := executor.Exec(ctx,
		"DELETE FROM environment_targets WHERE tenant_id = $1 AND project_id = $2 AND environment = $3",
		tenantID, projectID, env,
	)
	if err != nil {
		return fmt.Errorf("environment target store: delete target: %w", err)
	}
	return nil
}
