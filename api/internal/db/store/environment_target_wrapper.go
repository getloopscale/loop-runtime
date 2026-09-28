// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// environment_target_wrapper.go — sqlc-backed implementation of
// PostgresEnvironmentTargetStore. T2.1 Batch A #6 of plan
// 2026-05-29-sqlc-migration-plan v1.3.
//
// The 2 prod call sites (api/cmd/setup_stores.go:57,
// api/cmd/controller/setup_services.go:50) compile UNCHANGED — the
// constructor `db.NewPostgresEnvironmentTargetStore(dbAdapter,
// dbAdapter)` shape is preserved (constructor + struct in
// environment_target_types.go; methods here).
//
// Constructor design: the legacy store took separate Exec
// (ExecQueryExecutor) + Query (QueryExecutor) parameters so the
// caller could pass distinct adapters for writes vs reads. The sqlc-
// backed wrapper preserves the 2-arg constructor — both args are
// db.DBTX and BOTH must implement SqlcBridge. In practice the prod
// call sites pass the same *PgxAdapter for both; the field
// separation is preserved for backward symmetric API but the
// SqlcBridge dispatch unifies them at the sqlc.q.* invocation.
//
// Invariants preserved:
//   - SetDesired UPSERT (ON CONFLICT (tenant_id, project_id,
//     environment) DO UPDATE)
//   - SetActive runs AFTER ArgoCD confirms sync (caller discipline,
//     not enforced here)
//   - tx-required form for mutating methods (sqlctxprop gate
//     validates)
//   - Get NoRows → (nil, nil) per legacy contract
//   - ListByProject empty result → empty slice (not nil)

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/dbconv"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
)

// resolveSqlcDBTX selects the right db.DBTX (caller tx → fallback to
// store's Exec field) and bridges to generated.DBTX. The legacy
// pattern (`if tx != nil { executor = tx } else { executor = s.Exec
// }`) is preserved exactly — the wrapper only changes the dispatch
// target, not the selection logic.
func (s *PostgresEnvironmentTargetStore) resolveSqlcDBTX(tx db.DBTX) generated.DBTX {
	var target db.DBTX
	if tx != nil {
		target = tx
	} else {
		// Legacy field type was ExecQueryExecutor; we wrap with the
		// stored Exec which is now db.DBTX (widened in
		// environment_target_types.go). All 2 prod call sites pass
		// *PgxAdapter which satisfies both.
		target = s.Exec
	}
	return db.Generated(target)
}

func (s *PostgresEnvironmentTargetStore) queryBridge() generated.DBTX {
	return db.Generated(s.Query)
}

// SetDesired upserts environment_targets.desired_release_id. Runs
// inside the env-lock transaction (Rule 6).
func (s *PostgresEnvironmentTargetStore) SetDesired(ctx context.Context, tx db.DBTX, tenantID, projectID, env, releaseID, updatedBy, correlationID string) error {
	dbtx := s.resolveSqlcDBTX(tx)
	q := generated.New()
	if err := q.UpsertEnvironmentTargetDesired(ctx, dbtx, generated.UpsertEnvironmentTargetDesiredParams{
		TenantID:         tenantID,
		ProjectID:        projectID,
		Environment:      env,
		DesiredReleaseID: releaseID,
		UpdatedBy:        updatedBy,
		CorrelationID:    correlationID,
	}); err != nil {
		return fmt.Errorf("environment target store: set desired failed: %w", err)
	}
	return nil
}

// SetActive updates environment_targets.active_release_id after
// ArgoCD confirms sync.
func (s *PostgresEnvironmentTargetStore) SetActive(ctx context.Context, tx db.DBTX, tenantID, projectID, env, releaseID string) error {
	dbtx := s.resolveSqlcDBTX(tx)
	q := generated.New()
	if err := q.UpdateEnvironmentTargetActive(ctx, dbtx, generated.UpdateEnvironmentTargetActiveParams{
		ActiveReleaseID: dbconv.StringEmptyToNilPtr(releaseID),
		TenantID:        tenantID,
		ProjectID:       projectID,
		Environment:     env,
	}); err != nil {
		return fmt.Errorf("environment target store: set active failed: %w", err)
	}
	return nil
}

// Get retrieves the environment target for a (tenant, project,
// environment). Returns (nil, nil) when not found.
func (s *PostgresEnvironmentTargetStore) Get(ctx context.Context, tenantID, projectID, env string) (*EnvironmentTarget, error) {
	dbtx := s.queryBridge()
	q := generated.New()
	row, err := q.GetEnvironmentTarget(ctx, dbtx, generated.GetEnvironmentTargetParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("environment target store: get failed: %w", err)
	}
	return generatedEnvTargetToEnvTarget(row), nil
}

// ListByProject returns all environment targets for a (tenant,
// project). Empty result → empty slice (not nil) per legacy contract.
func (s *PostgresEnvironmentTargetStore) ListByProject(ctx context.Context, tenantID, projectID string) ([]EnvironmentTarget, error) {
	dbtx := s.queryBridge()
	q := generated.New()
	rows, err := q.ListEnvironmentTargetsByProject(ctx, dbtx, generated.ListEnvironmentTargetsByProjectParams{
		TenantID:  tenantID,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("environment target store: list by project: %w", err)
	}
	targets := make([]EnvironmentTarget, 0, len(rows))
	for _, r := range rows {
		targets = append(targets, *generatedEnvTargetToEnvTarget(r))
	}
	return targets, nil
}

// DeleteTarget removes an environment target within a transaction.
// Idempotent — missing target → no-op.
func (s *PostgresEnvironmentTargetStore) DeleteTarget(ctx context.Context, tx db.DBTX, tenantID, projectID, env string) error {
	dbtx := s.resolveSqlcDBTX(tx)
	q := generated.New()
	if err := q.DeleteEnvironmentTarget(ctx, dbtx, generated.DeleteEnvironmentTargetParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
	}); err != nil {
		return fmt.Errorf("environment target store: delete target: %w", err)
	}
	return nil
}

// SetPreviewDesired upserts an environment_targets row with
// kind='preview'. D2 of plan 2026-06-07-preview-as-category.
// PreviewService calls this after a preview build commits its
// release. The kind column is immutable post-create — repeat inserts
// for the same (tenant, project, environment) update release tracking
// but keep kind='preview'.
func (s *PostgresEnvironmentTargetStore) SetPreviewDesired(ctx context.Context, tx db.DBTX, tenantID, projectID, env, releaseID, updatedBy, correlationID string) error {
	dbtx := s.resolveSqlcDBTX(tx)
	q := generated.New()
	if err := q.UpsertPreviewEnvironmentTarget(ctx, dbtx, generated.UpsertPreviewEnvironmentTargetParams{
		TenantID:         tenantID,
		ProjectID:        projectID,
		Environment:      env,
		DesiredReleaseID: releaseID,
		UpdatedBy:        updatedBy,
		CorrelationID:    correlationID,
	}); err != nil {
		return fmt.Errorf("environment target store: set preview desired: %w", err)
	}
	return nil
}

// CountPermanent returns the number of DISTINCT permanent envs for a
// tenant across all projects. D5 (Bug A fix): used by the enforcement
// limiter to gate POST /api/v1/environments against
// plan_limits.environments cap. Preview envs are not counted.
func (s *PostgresEnvironmentTargetStore) CountPermanent(ctx context.Context, tenantID string) (int, error) {
	dbtx := s.queryBridge()
	q := generated.New()
	n, err := q.CountPermanentEnvironments(ctx, dbtx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("environment target store: count permanent: %w", err)
	}
	return int(n), nil
}

// ListPermanent returns the DISTINCT permanent envs for a tenant,
// alphabetically sorted. D7 helper for the dashboard /environments
// grid (Bug B fix).
func (s *PostgresEnvironmentTargetStore) ListPermanent(ctx context.Context, tenantID string) ([]string, error) {
	dbtx := s.queryBridge()
	q := generated.New()
	rows, err := q.ListPermanentEnvironments(ctx, dbtx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("environment target store: list permanent: %w", err)
	}
	// Empty result → empty slice (not nil) per legacy contract.
	if rows == nil {
		return []string{}, nil
	}
	return rows, nil
}

// DeleteByKind removes a single (tenant, project, environment) row
// when its kind column matches. D6 cleanup chain: used by
// PreviewService.DeletePreviewByPR. Idempotent.
func (s *PostgresEnvironmentTargetStore) DeleteByKind(ctx context.Context, tx db.DBTX, tenantID, projectID, env, kind string) error {
	dbtx := s.resolveSqlcDBTX(tx)
	q := generated.New()
	if err := q.DeleteEnvironmentTargetsByKind(ctx, dbtx, generated.DeleteEnvironmentTargetsByKindParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
		Kind:        kind,
	}); err != nil {
		return fmt.Errorf("environment target store: delete by kind: %w", err)
	}
	return nil
}

// generatedEnvTargetToEnvTarget converts a sqlc-generated row into
// the public type. pgtype.Timestamptz.Valid → time.Time (zero time
// when invalid). The Get/ListByProject queries SELECT all columns
// (including `kind`) so Row types alias to generated.EnvironmentTarget
// — accepting that single type keeps this helper minimal.
func generatedEnvTargetToEnvTarget(row generated.EnvironmentTarget) *EnvironmentTarget {
	t := &EnvironmentTarget{
		TenantID:         row.TenantID,
		ProjectID:        row.ProjectID,
		Environment:      row.Environment,
		DesiredReleaseID: row.DesiredReleaseID,
		ActiveReleaseID:  row.ActiveReleaseID,
		UpdatedBy:        row.UpdatedBy,
		CorrelationID:    row.CorrelationID,
		Kind:             row.Kind,
	}
	if row.UpdatedAt.Valid {
		t.UpdatedAt = row.UpdatedAt.Time
	}
	return t
}

// RecordReplicasReady writes the data-plane readiness snapshot of one
// (tenant, project, environment): the ready replica count and when it was
// observed. It touches only those two observation columns — never the
// release pointers the environment lock guards — so it runs outside the
// lock. found is false when no target row matches, which the caller must
// report: a snapshot that is never written silently goes stale.
//
// Cross-tenant: build the store on the background (loopzero_worker) pool.
func (s *PostgresEnvironmentTargetStore) RecordReplicasReady(ctx context.Context, tenantID, projectID, env string, replicasReady int32, at time.Time) (found bool, err error) {
	q := generated.New()
	rows, err := q.RecordEnvironmentTargetReplicasReady(ctx, s.resolveSqlcDBTX(nil), generated.RecordEnvironmentTargetReplicasReadyParams{
		LastReplicasReadyValue: &replicasReady,
		LastReplicasReadyAt:    dbconv.TimeToTimestamptz(at),
		TenantID:               tenantID,
		ProjectID:              projectID,
		Environment:            env,
	})
	if err != nil {
		return false, fmt.Errorf("environment target store: record replicas ready for %s/%s/%s: %w", tenantID, projectID, env, err)
	}
	return rows > 0, nil
}
