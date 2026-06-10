// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

// environment_target_wrapper.go — sqlc-backed implementation of
// PostgresEnvironmentTargetStore. T2.1 Batch A #6 of plan
// docs/plans/2026-05-29-sqlc-migration-plan.md v1.3.
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

	"github.com/jackc/pgx/v5"

	"github.com/loopzero/loopzero/api/internal/db/generated"
)

// resolveSqlcDBTX selects the right db.DBTX (caller tx → fallback to
// store's Exec field) and bridges to generated.DBTX. The legacy
// pattern (`if tx != nil { executor = tx } else { executor = s.Exec
// }`) is preserved exactly — the wrapper only changes the dispatch
// target, not the selection logic.
func (s *PostgresEnvironmentTargetStore) resolveSqlcDBTX(tx DBTX) (generated.DBTX, error) {
	var target DBTX
	if tx != nil {
		target = tx
	} else {
		// Legacy field type was ExecQueryExecutor; we wrap with the
		// stored Exec which is now db.DBTX (widened in
		// environment_target_types.go). All 2 prod call sites pass
		// *PgxAdapter which satisfies both.
		target = s.Exec
	}
	bridge, ok := target.(SqlcBridge)
	if !ok {
		return nil, fmt.Errorf("environment target store: DBTX %T does not implement SqlcBridge", target)
	}
	return bridge.GeneratedDB(), nil
}

func (s *PostgresEnvironmentTargetStore) queryBridge() (generated.DBTX, error) {
	bridge, ok := s.Query.(SqlcBridge)
	if !ok {
		return nil, fmt.Errorf("environment target store: Query %T does not implement SqlcBridge", s.Query)
	}
	return bridge.GeneratedDB(), nil
}

// SetDesired upserts environment_targets.desired_release_id. Runs
// inside the env-lock transaction (Rule 6).
func (s *PostgresEnvironmentTargetStore) SetDesired(ctx context.Context, tx DBTX, tenantID, projectID, env, releaseID, updatedBy, correlationID string) error {
	dbtx, err := s.resolveSqlcDBTX(tx)
	if err != nil {
		return err
	}
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
func (s *PostgresEnvironmentTargetStore) SetActive(ctx context.Context, tx DBTX, tenantID, projectID, env, releaseID string) error {
	dbtx, err := s.resolveSqlcDBTX(tx)
	if err != nil {
		return err
	}
	q := generated.New()
	if err := q.UpdateEnvironmentTargetActive(ctx, dbtx, generated.UpdateEnvironmentTargetActiveParams{
		ActiveReleaseID: stringEmptyToNilPtr(releaseID),
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
	dbtx, err := s.queryBridge()
	if err != nil {
		return nil, err
	}
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
	dbtx, err := s.queryBridge()
	if err != nil {
		return nil, err
	}
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
func (s *PostgresEnvironmentTargetStore) DeleteTarget(ctx context.Context, tx DBTX, tenantID, projectID, env string) error {
	dbtx, err := s.resolveSqlcDBTX(tx)
	if err != nil {
		return err
	}
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

// generatedEnvTargetToEnvTarget converts a sqlc-generated row into
// the public type. pgtype.Timestamptz.Valid → time.Time (zero time
// when invalid).
func generatedEnvTargetToEnvTarget(row generated.EnvironmentTarget) *EnvironmentTarget {
	t := &EnvironmentTarget{
		TenantID:         row.TenantID,
		ProjectID:        row.ProjectID,
		Environment:      row.Environment,
		DesiredReleaseID: row.DesiredReleaseID,
		ActiveReleaseID:  row.ActiveReleaseID,
		UpdatedBy:        row.UpdatedBy,
		CorrelationID:    row.CorrelationID,
	}
	if row.UpdatedAt.Valid {
		t.UpdatedAt = row.UpdatedAt.Time
	}
	return t
}
