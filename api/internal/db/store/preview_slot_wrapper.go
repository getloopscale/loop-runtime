// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// preview_slot_wrapper.go — sqlc-backed implementation of
// PostgresPreviewSlotStore. Created 2026-06-07 by plan
// 2026-06-07-preview-as-category-single-slot-plan.md (Phase 1).
//
// Symmetric with environment_target_wrapper.go: bridges DBTX (caller
// tx OR fallback Exec/Query field) through SqlcBridge.GeneratedDB()
// to the sqlc-generated query layer.
//
// Invariants:
//   - Upsert/Clear/EnsureEmpty are tx-required (run inside the env
//     lock; sqlctxprop gate validates)
//   - Get NoRows → (nil, nil) per legacy contract
//   - Upsert pattern follows ON CONFLICT (tenant_id) DO UPDATE for
//     atomic substitution (D3)

import (
	"context"
	"errors"
	"fmt"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/dbconv"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
)

// resolveSqlcDBTX selects the right DBTX (caller tx → fallback Exec)
// and bridges to generated.DBTX.
func (s *PostgresPreviewSlotStore) resolveSqlcDBTX(tx db.DBTX) generated.DBTX {
	var target db.DBTX
	if tx != nil {
		target = tx
	} else {
		target = s.Exec
	}
	return db.Generated(target)
}

func (s *PostgresPreviewSlotStore) queryBridge() generated.DBTX {
	return db.Generated(s.Query)
}

// Get retrieves the preview slot for a (tenant, project). (nil, nil)
// on NoRows.
func (s *PostgresPreviewSlotStore) Get(ctx context.Context, tenantID, projectID string) (*PreviewSlot, error) {
	dbtx := s.queryBridge()
	q := generated.New()
	row, err := q.GetPreviewSlot(ctx, dbtx, generated.GetPreviewSlotParams{
		TenantID:  tenantID,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("preview slot store: get failed: %w", err)
	}
	return generatedPreviewSlotToSlot(row), nil
}

// ListByTenant returns all preview slots for a tenant, one per
// project. Empty result → empty slice (not nil) per legacy contract.
func (s *PostgresPreviewSlotStore) ListByTenant(ctx context.Context, tenantID string) ([]PreviewSlot, error) {
	dbtx := s.queryBridge()
	q := generated.New()
	rows, err := q.ListPreviewSlotsByTenant(ctx, dbtx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("preview slot store: list by tenant failed: %w", err)
	}
	out := make([]PreviewSlot, 0, len(rows))
	for _, row := range rows {
		out = append(out, *generatedPreviewSlotToSlot(row))
	}
	return out, nil
}

// generatedPreviewSlotToSlot converts a sqlc-generated row into the
// public type. Centralized so Get + ListByTenant share the same
// projection — adding a column means one helper edit, not two.
func generatedPreviewSlotToSlot(row generated.PreviewSlot) *PreviewSlot {
	slot := &PreviewSlot{
		TenantID:        row.TenantID,
		ProjectID:       row.ProjectID,
		ActivePRNumber:  row.ActivePrNumber,
		ActiveBranch:    row.ActiveBranch,
		ActiveReleaseID: row.ActiveReleaseID,
		URL:             row.Url,
	}
	if row.UpdatedAt.Valid {
		slot.UpdatedAt = row.UpdatedAt.Time
	}
	return slot
}

// Upsert atomically inserts or replaces the slot. D3 substitute
// behavior: ON CONFLICT (tenant_id) DO UPDATE replaces the previous
// active PR with the new one.
func (s *PostgresPreviewSlotStore) Upsert(ctx context.Context, tx db.DBTX, tenantID, projectID string, activePRNumber int32, activeBranch, activeReleaseID, url string) error {
	dbtx := s.resolveSqlcDBTX(tx)
	prNum := activePRNumber
	q := generated.New()
	if err := q.UpsertPreviewSlot(ctx, dbtx, generated.UpsertPreviewSlotParams{
		TenantID:        tenantID,
		ProjectID:       projectID,
		ActivePrNumber:  &prNum,
		ActiveBranch:    dbconv.StringEmptyToNilPtr(activeBranch),
		ActiveReleaseID: dbconv.StringEmptyToNilPtr(activeReleaseID),
		Url:             dbconv.StringEmptyToNilPtr(url),
	}); err != nil {
		return fmt.Errorf("preview slot store: upsert failed: %w", err)
	}
	return nil
}

// Clear NULLs the active_* fields of a (tenant, project) slot.
// Idempotent.
func (s *PostgresPreviewSlotStore) Clear(ctx context.Context, tx db.DBTX, tenantID, projectID string) error {
	dbtx := s.resolveSqlcDBTX(tx)
	q := generated.New()
	if err := q.ClearPreviewSlot(ctx, dbtx, generated.ClearPreviewSlotParams{
		TenantID:  tenantID,
		ProjectID: projectID,
	}); err != nil {
		return fmt.Errorf("preview slot store: clear failed: %w", err)
	}
	return nil
}

// EnsureEmpty inserts an empty slot if absent, else clears active_*.
// Used by tenant provision (D1 — preview category always present).
func (s *PostgresPreviewSlotStore) EnsureEmpty(ctx context.Context, tx db.DBTX, tenantID, projectID string) error {
	dbtx := s.resolveSqlcDBTX(tx)
	q := generated.New()
	if err := q.EnsurePreviewSlotEmpty(ctx, dbtx, generated.EnsurePreviewSlotEmptyParams{
		TenantID:  tenantID,
		ProjectID: projectID,
	}); err != nil {
		return fmt.Errorf("preview slot store: ensure empty failed: %w", err)
	}
	return nil
}
