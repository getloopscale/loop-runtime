// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// cell_wrapper.go — sqlc-backed implementation of PostgresCellStore.
// T2.2 (Batch B #2) of plan 2026-05-29-sqlc-migration-plan
// v1.3.
//
// 4 methods dispatch through SqlcBridge:
//   - CellTenantCounts: aggregation across tenants (ADR-0008 Phase 0)
//   - CountByCell: alias of CellTenantCounts satisfying
//     placement.TenantCellQuerier
//   - GetTenantCellID: tenant → cell lookup (DynamicRouter feed)
//   - PinTenantToCell: 4-step admin override in one transaction under
//     the placement advisory lock (CHG-2026-04-30-DELETE-SINGLE-CELL)
//
// Invariants preserved:
//   - CellTenantCounts/CountByCell returns non-nil empty map when zero
//     rows
//   - GetTenantCellID wraps errors with "cell store: get tenant
//     cell_id:" prefix (legacy contract)
//   - PinTenantToCell sequence + error mapping verbatim:
//     1. tenant exists → ErrCellPinnerTenantNotFound
//     2. cell exists → ErrCellPinnerCellNotFound
//     3. cell.status == 'active' → ErrCellPinnerNotPinnable
//     4. capacity check (NULL = unlimited; excludes moving tenant) →
//        ErrCellPinnerAtCapacity
//     5. UPDATE → returns previous cell_id

import (
	"context"
	"errors"
	"fmt"

	"github.com/getloopscale/loopzero/api/internal/db"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loopzero/api/internal/db/dbtx"
	"github.com/getloopscale/loopzero/api/internal/db/generated"
)

// CellTenantCounts returns the number of tenants per cell_id.
// Used by CellMetricsCollector to emit loopzero_cell_tenant_count gauge.
func (s *PostgresCellStore) CellTenantCounts(ctx context.Context) (map[string]int, error) {
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.CountTenantsByCellID(ctx, bridge)
	if err != nil {
		return nil, fmt.Errorf("cell store: query tenant counts: %w", err)
	}

	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.CellID] = int(row.TenantCount)
	}
	return counts, nil
}

// CountByCell is an alias for CellTenantCounts that satisfies
// the placement.TenantCellQuerier interface.
func (s *PostgresCellStore) CountByCell(ctx context.Context) (map[string]int, error) {
	return s.CellTenantCounts(ctx)
}

// GetTenantCellID returns the cell_id for a specific tenant.
// Returns error if the tenant does not exist (no transparent nil to
// distinguish missing tenant from cross-cluster fault).
func (s *PostgresCellStore) GetTenantCellID(ctx context.Context, tenantID string) (string, error) {
	bridge := s.bridge()
	q := generated.New()
	cellID, err := q.GetCellIDByTenantID(ctx, bridge, tenantID)
	if err != nil {
		return "", fmt.Errorf("cell store: get tenant cell_id: %w", err)
	}
	return cellID, nil
}

// PinTenantToCell implements placement.CellPinnerStore. The whole sequence
// runs in ONE transaction that first takes the placement advisory lock
// (dbtx.PlacementLockID, the key the placement engine assigns cells under):
//  1. validate tenant exists → ErrCellPinnerTenantNotFound
//  2. validate cell exists + status='active' → ErrCellPinnerCellNotFound/
//     ErrCellPinnerNotPinnable
//  3. capacity check (NULL = unlimited; current excludes moving tenant)
//     → ErrCellPinnerAtCapacity
//  4. UPDATE tenants.cell_id, returning the previous cell_id
//
// Steps 3 and 4 are a check-then-act: run as independent pooled statements,
// two concurrent pins (or a pin and a new-tenant placement) could both read
// "room for one" and both move in. Holding the placement lock across all four
// makes the capacity read and the write one serialized decision.
func (s *PostgresCellStore) PinTenantToCell(ctx context.Context, tenantID, cellID string) (string, error) {
	adapter, ok := s.DB.(*db.PgxAdapter)
	if !ok || adapter.Pool == nil {
		return "", fmt.Errorf("cell store: pinning needs a pgx pool to hold the placement lock, got DBTX %T", s.DB)
	}
	tx, err := adapter.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("cell store: begin pin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit

	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '30s'"); err != nil {
		return "", fmt.Errorf("cell store: set lock_timeout: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", dbtx.PlacementLockID); err != nil {
		return "", fmt.Errorf("cell store: acquire placement lock: %w", err)
	}

	previous, err := pinTenantToCellTx(ctx, tx, tenantID, cellID)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("cell store: commit pin: %w", err)
	}
	return previous, nil
}

// pinTenantToCellTx runs the four pin steps on a transaction that already
// holds the placement lock.
func pinTenantToCellTx(ctx context.Context, tx generated.DBTX, tenantID, cellID string) (string, error) {
	q := generated.New()

	existingCell, err := q.LookupTenantForCellPin(ctx, tx, tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrCellPinnerTenantNotFound
		}
		return "", fmt.Errorf("cell store: lookup tenant: %w", err)
	}

	cell, err := q.LookupCellForPin(ctx, tx, cellID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrCellPinnerCellNotFound
		}
		return "", fmt.Errorf("cell store: lookup cell: %w", err)
	}
	if cell.Status != "active" {
		return "", ErrCellPinnerNotPinnable
	}
	if cell.CapacityTenantsMax != nil {
		current, err := q.CountTenantsInCellExcluding(ctx, tx, generated.CountTenantsInCellExcludingParams{
			CellID: cellID,
			ID:     tenantID,
		})
		if err != nil {
			return "", fmt.Errorf("cell store: count target cell: %w", err)
		}
		if current >= int64(*cell.CapacityTenantsMax) {
			return "", ErrCellPinnerAtCapacity
		}
	}

	if err := q.UpdateTenantCellID(ctx, tx, generated.UpdateTenantCellIDParams{
		CellID: cellID,
		ID:     tenantID,
	}); err != nil {
		return "", fmt.Errorf("cell store: update tenant: %w", err)
	}
	return existingCell, nil
}

// bridge returns the sqlc-compatible DBTX from s.DB.
func (s *PostgresCellStore) bridge() generated.DBTX {
	return db.Generated(s.DB)
}
