// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// scaling_wrapper.go — sqlc-backed implementation of
// PostgresScalingStore. T2.1 Batch A #7 of plan
// 2026-05-29-sqlc-migration-plan v1.3.
//
// The 1 prod call site (api/cmd/setup_orchestrator.go:74) compiles
// UNCHANGED — the constructor `db.NewPostgresScalingStore(dbAdapter)`
// is preserved (constructor + struct + ScalingConfig +
// ScalingStore interface + ErrScalingConfigNotFound +
// CanonicalHash/Validate methods in scaling_types.go).
//
// Invariants preserved:
//   - Upsert idempotent short-circuit when ConfigHash matches stored
//     row (no DB write; returns (stored, false, nil))
//   - Upsert UPSERT (ON CONFLICT (tenant_id, project_id,
//     environment, app_name) DO UPDATE)
//   - GetByApp NoRows → ErrScalingConfigNotFound (legacy sentinel)
//   - Delete idempotent (silent on missing row)
//   - DeleteByProject nil-tx fallback to s.db preserved
//   - Validate runs BEFORE any DB call (LoopZeroError actionable
//     feedback)
//   - CanonicalHash deterministic format preserved verbatim in
//     scaling_types.go

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/getloopscale/loop-runtime/api/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
)

// pgtypeUUIDToString formats a pgtype.UUID into the canonical
// 8-4-4-4-12 hex form. Returns "" when not valid (NULL semantics).
// The legacy ScalingConfig.ID is a plain string; sqlc emits
// pgtype.UUID for the uuid column — this helper bridges the
// difference at the wrapper seam.
func pgtypeUUIDToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	h := hex.EncodeToString(u.Bytes[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// GetByApp implements ScalingStore.
func (s *PostgresScalingStore) GetByApp(ctx context.Context, tenantID, projectID, environment, appName string) (*ScalingConfig, error) {
	gdb := db.Generated(s.db)
	q := generated.New()

	row, err := q.GetScalingConfigByApp(ctx, gdb, generated.GetScalingConfigByAppParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: environment,
		AppName:     appName,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrScalingConfigNotFound
		}
		return nil, fmt.Errorf("scaling_store: GetByApp: %w", err)
	}
	return generatedScalingToScalingConfig(row), nil
}

// Upsert implements ScalingStore. Returns (final, changed, err).
// Idempotent short-circuit when ConfigHash matches existing.
func (s *PostgresScalingStore) Upsert(ctx context.Context, tx db.DBTX, incoming *ScalingConfig) (*ScalingConfig, bool, error) {
	if err := incoming.Validate(); err != nil {
		return nil, false, err
	}
	incoming.ConfigHash = incoming.CanonicalHash()

	// Read current state for hash comparison.
	existing, err := s.GetByApp(ctx, incoming.TenantID, incoming.ProjectID, incoming.Environment, incoming.AppName)
	if err != nil && !errors.Is(err, ErrScalingConfigNotFound) {
		return nil, false, err
	}
	if existing != nil && existing.ConfigHash == incoming.ConfigHash {
		return existing, false, nil
	}

	gdb := db.Generated(tx)
	q := generated.New()

	out, err := q.UpsertScalingConfig(ctx, gdb, generated.UpsertScalingConfigParams{
		TenantID:        incoming.TenantID,
		ProjectID:       incoming.ProjectID,
		Environment:     incoming.Environment,
		AppName:         incoming.AppName,
		MinReplicas:     incoming.MinReplicas,
		MaxReplicas:     incoming.MaxReplicas,
		TargetCpuPct:    incoming.TargetCPUPct,
		TargetMemoryPct: incoming.TargetMemoryPct,
		ScaleToZero:     incoming.ScaleToZero,
		UpdatedBy:       incoming.UpdatedBy,
		ConfigHash:      incoming.ConfigHash,
	})
	if err != nil {
		return nil, false, fmt.Errorf("scaling_store: Upsert: %w", err)
	}
	incoming.ID = pgtypeUUIDToString(out.ID)
	if out.UpdatedAt.Valid {
		incoming.UpdatedAt = out.UpdatedAt.Time
	}
	return incoming, true, nil
}

// ListByEnv implements ScalingStore.
func (s *PostgresScalingStore) ListByEnv(ctx context.Context, tenantID, projectID, environment string) ([]ScalingConfig, error) {
	gdb := db.Generated(s.db)
	q := generated.New()

	rows, err := q.ListScalingConfigsByEnv(ctx, gdb, generated.ListScalingConfigsByEnvParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: environment,
	})
	if err != nil {
		return nil, fmt.Errorf("scaling_store: ListByEnv: %w", err)
	}
	out := make([]ScalingConfig, 0, len(rows))
	for _, r := range rows {
		out = append(out, *generatedScalingToScalingConfig(r))
	}
	return out, nil
}

// Delete implements ScalingStore. Idempotent.
func (s *PostgresScalingStore) Delete(ctx context.Context, tx db.DBTX, tenantID, projectID, environment, appName string) error {
	gdb := db.Generated(tx)
	q := generated.New()

	if err := q.DeleteScalingConfig(ctx, gdb, generated.DeleteScalingConfigParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: environment,
		AppName:     appName,
	}); err != nil {
		return fmt.Errorf("scaling_store: Delete: %w", err)
	}
	return nil
}

// DeleteByProject implements ScalingStore. Cascade for project
// deletion. Nil-tx fallback to s.db preserved (ProjectDeleter
// pattern).
func (s *PostgresScalingStore) DeleteByProject(ctx context.Context, tx db.DBTX, tenantID, projectID string) error {
	target := tx
	if target == nil {
		target = s.db
	}
	gdb := db.Generated(target)
	q := generated.New()

	if err := q.DeleteScalingConfigsByProject(ctx, gdb, generated.DeleteScalingConfigsByProjectParams{
		TenantID:  tenantID,
		ProjectID: projectID,
	}); err != nil {
		return fmt.Errorf("scaling_store: DeleteByProject: %w", err)
	}
	return nil
}

// CountByTenant implements ScalingStore. Cast int64 → int (legacy
// signature).
func (s *PostgresScalingStore) CountByTenant(ctx context.Context, tenantID string) (int, error) {
	gdb := db.Generated(s.db)
	q := generated.New()

	n, err := q.CountScalingConfigsByTenant(ctx, gdb, tenantID)
	if err != nil {
		return 0, fmt.Errorf("scaling_store: CountByTenant: %w", err)
	}
	return int(n), nil
}

// generatedScalingToScalingConfig converts a sqlc-generated row into
// the public type. pgtype.Timestamptz.Valid → time.Time.
func generatedScalingToScalingConfig(row generated.AppScalingConfig) *ScalingConfig {
	c := &ScalingConfig{
		ID:              pgtypeUUIDToString(row.ID),
		TenantID:        row.TenantID,
		ProjectID:       row.ProjectID,
		Environment:     row.Environment,
		AppName:         row.AppName,
		MinReplicas:     row.MinReplicas,
		MaxReplicas:     row.MaxReplicas,
		TargetCPUPct:    row.TargetCpuPct,
		TargetMemoryPct: row.TargetMemoryPct,
		ScaleToZero:     row.ScaleToZero,
		UpdatedBy:       row.UpdatedBy,
		ConfigHash:      row.ConfigHash,
	}
	if row.UpdatedAt.Valid {
		c.UpdatedAt = row.UpdatedAt.Time
	}
	return c
}
