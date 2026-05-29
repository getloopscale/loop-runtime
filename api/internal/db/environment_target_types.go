// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

// environment_target_types.go — public types for the
// environment_targets domain that survive the T2.1 (Batch A #6)
// deletion of the legacy `environment_target_store.go`. The 2 prod
// call sites use the constructor — preserved here.

import (
	"context"
	"time"
)

// EnvironmentTarget represents a row in the environment_targets
// table. It tracks which release is desired/active for each (tenant,
// project, environment). See spec §9 — environment_targets table,
// §3 — Source of Truth.
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

// EnvironmentTargetStore defines the interface for environment
// target operations. All tx-accepting methods (SetDesired,
// SetActive, DeleteTarget) run inside an existing transaction from
// withEnvironmentLock — they NEVER start their own.
type EnvironmentTargetStore interface {
	SetDesired(ctx context.Context, tx DBTX, tenantID, projectID, env, releaseID, updatedBy, correlationID string) error
	SetActive(ctx context.Context, tx DBTX, tenantID, projectID, env, releaseID string) error
	Get(ctx context.Context, tenantID, projectID, env string) (*EnvironmentTarget, error)
	ListByProject(ctx context.Context, tenantID, projectID string) ([]EnvironmentTarget, error)
	DeleteTarget(ctx context.Context, tx DBTX, tenantID, projectID, env string) error
}

// PostgresEnvironmentTargetStore implements EnvironmentTargetStore
// using PostgreSQL via sqlc. Exec is used for mutating methods (runs
// inside the lock transaction); Query is used for reads (can read
// outside a transaction).
//
// T2.1 Batch A #6 (plan 2026-05-29-sqlc-migration v1.3): both
// fields widened from ExecQueryExecutor / QueryExecutor (the legacy
// intersection interfaces) to the standard db.DBTX so the sqlc
// dispatch can bridge through SqlcBridge. Both prod call sites pass
// the same *PgxAdapter; the dual-field design is preserved for
// symmetric API.
type PostgresEnvironmentTargetStore struct {
	Exec  DBTX
	Query DBTX
}

// NewPostgresEnvironmentTargetStore creates a new
// PostgresEnvironmentTargetStore. Preserved verbatim from legacy so
// the 2 prod call sites compile unchanged.
func NewPostgresEnvironmentTargetStore(exec DBTX, query DBTX) *PostgresEnvironmentTargetStore {
	return &PostgresEnvironmentTargetStore{
		Exec:  exec,
		Query: query,
	}
}

// Compile-time assertion: PostgresEnvironmentTargetStore satisfies
// EnvironmentTargetStore 1:1.
var _ EnvironmentTargetStore = (*PostgresEnvironmentTargetStore)(nil)
