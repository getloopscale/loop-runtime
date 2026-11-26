// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// environment_target_types.go — public types for the
// environment_targets domain that survive the T2.1 (Batch A #6)
// deletion of the legacy `environment_target_store.go`. The 2 prod
// call sites use the constructor — preserved here.

import (
	"context"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// Environment target kinds (D2 of plan 2026-06-07-preview-as-category).
// Used by D5 enforcement to filter permanent envs only when counting
// against plan_limits.environments cap, and by D7 dashboard merge.
const (
	EnvironmentTargetKindPermanent = "permanent"
	EnvironmentTargetKindPreview   = "preview"
)

// EnvironmentTarget represents a row in the environment_targets
// table. It tracks which release is desired/active for each (tenant,
// project, environment). See spec §9 — environment_targets table,
// §3 — Source of Truth.
//
// 2026-06-07 D2: `Kind` added by migration 000027 (CHECK in
// 'permanent' | 'preview'). Defaults to 'permanent' for backward-
// compatible inserts via SetDesired.
type EnvironmentTarget struct {
	TenantID         string    `json:"tenant_id"`
	ProjectID        string    `json:"project_id"`
	Environment      string    `json:"environment"`
	DesiredReleaseID string    `json:"desired_release_id"`
	ActiveReleaseID  *string   `json:"active_release_id,omitempty"`
	UpdatedBy        string    `json:"updated_by"`
	CorrelationID    string    `json:"correlation_id"`
	UpdatedAt        time.Time `json:"updated_at"`
	Kind             string    `json:"kind"`
}

// EnvironmentTargetStore defines the interface for environment
// target operations. All tx-accepting methods (SetDesired,
// SetActive, DeleteTarget, SetPreviewDesired, DeleteByKind) run
// inside an existing transaction from withEnvironmentLock — they
// NEVER start their own.
type EnvironmentTargetStore interface {
	SetDesired(ctx context.Context, tx db.DBTX, tenantID, projectID, env, releaseID, updatedBy, correlationID string) error
	SetActive(ctx context.Context, tx db.DBTX, tenantID, projectID, env, releaseID string) error
	Get(ctx context.Context, tenantID, projectID, env string) (*EnvironmentTarget, error)
	ListByProject(ctx context.Context, tenantID, projectID string) ([]EnvironmentTarget, error)
	DeleteTarget(ctx context.Context, tx db.DBTX, tenantID, projectID, env string) error

	// D2: SetPreviewDesired upserts an env_target row with
	// kind='preview'. Distinct from SetDesired (defaults
	// kind='permanent') because once set, kind is immutable
	// post-create (D2 consequence).
	SetPreviewDesired(ctx context.Context, tx db.DBTX, tenantID, projectID, env, releaseID, updatedBy, correlationID string) error

	// D5 (Bug A fix): CountPermanent returns the number of distinct
	// PERMANENT environments for a tenant across all projects. Used
	// by pkg/enforcement/limiter.go to gate POST /api/v1/environments
	// against plan_limits.environments cap. Preview environments are
	// NOT counted (single-slot model).
	CountPermanent(ctx context.Context, tenantID string) (int, error)

	// D7 (Bug B fix): ListPermanent returns the DISTINCT permanent
	// environments for a tenant, alphabetically sorted. Used by the
	// dashboard /environments grid which merges this with the
	// GetPreviewSlot result into the {permanent[], preview{}}
	// response.
	ListPermanent(ctx context.Context, tenantID string) ([]string, error)

	// D6 cleanup chain: DeleteByKind removes a single (tenant,
	// project, environment) row when its kind matches. Idempotent
	// (DELETE matches 0 or 1 row). Used by
	// PreviewService.DeletePreviewByPR to remove preview env_target
	// rows on PR close.
	DeleteByKind(ctx context.Context, tx db.DBTX, tenantID, projectID, env, kind string) error
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
	Exec  db.DBTX
	Query db.DBTX
}

// NewPostgresEnvironmentTargetStore creates a new
// PostgresEnvironmentTargetStore. Preserved verbatim from legacy so
// the 2 prod call sites compile unchanged.
func NewPostgresEnvironmentTargetStore(exec db.DBTX, query db.DBTX) *PostgresEnvironmentTargetStore {
	return &PostgresEnvironmentTargetStore{
		Exec:  exec,
		Query: query,
	}
}

// Compile-time assertion: PostgresEnvironmentTargetStore satisfies
// EnvironmentTargetStore 1:1.
var _ EnvironmentTargetStore = (*PostgresEnvironmentTargetStore)(nil)
