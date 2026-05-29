// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

// cell_wrapper.go — sqlc-backed implementation of PostgresCellStore.
// T2.2 (Batch B #2) of plan docs/plans/2026-05-29-sqlc-migration-plan.md
// v1.3.
//
// 4 methods dispatch through SqlcBridge:
//   - CellTenantCounts: aggregation across tenants (ADR-0008 Phase 0)
//   - CountByCell: alias of CellTenantCounts satisfying
//     placement.TenantCellQuerier
//   - GetTenantCellID: tenant → cell lookup (DynamicRouter feed)
//   - PinTenantToCell: atomic 4-step admin override
//     (CHG-2026-04-30-DELETE-SINGLE-CELL)
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

	"github.com/jackc/pgx/v5"

	"github.com/loopzero/loopzero/api/internal/db/generated"
)

// CellTenantCounts returns the number of tenants per cell_id.
// Used by CellMetricsCollector to emit loopzero_cell_tenant_count gauge.
func (s *PostgresCellStore) CellTenantCounts(ctx context.Context) (map[string]int, error) {
	bridge, err := s.bridge()
	if err != nil {
		return nil, err
	}
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
	bridge, err := s.bridge()
	if err != nil {
		return "", err
	}
	q := generated.New()
	cellID, err := q.GetCellIDByTenantID(ctx, bridge, tenantID)
	if err != nil {
		return "", fmt.Errorf("cell store: get tenant cell_id: %w", err)
	}
	return cellID, nil
}

// PinTenantToCell implements services.CellPinnerStore. Atomic
// 4-step sequence:
//   1. validate tenant exists → ErrCellPinnerTenantNotFound
//   2. validate cell exists + status='active' → ErrCellPinnerCellNotFound/
//      ErrCellPinnerNotPinnable
//   3. capacity check (NULL = unlimited; current excludes moving tenant)
//      → ErrCellPinnerAtCapacity
//   4. UPDATE tenants.cell_id, RETURNING previous cell_id
//
// CHG-2026-04-30-DELETE-SINGLE-CELL: wires admin tenant-pin endpoint
// previously dormant in router.go.
//
// Implementation notes: DBTX is the abstraction loopzero-api uses across
// pool/tx, so this method runs without an explicit SERIALIZABLE wrapper.
// Correctness relies on:
//   1. Pre-flight existence checks for tenant + cell (deterministic errors)
//   2. status='active' check before capacity (cheap rejects fast)
//   3. capacity check excludes the moving tenant (re-pin-to-same-cell is OK)
//   4. UPDATE is atomic at the row level
//
// Concurrent admin pin races for the same tenant are rare (operator-driven),
// and the application-level placement lock used during tenant creation is
// the primary serialization mechanism.
func (s *PostgresCellStore) PinTenantToCell(ctx context.Context, tenantID, cellID string) (string, error) {
	bridge, err := s.bridge()
	if err != nil {
		return "", err
	}
	q := generated.New()

	existingCell, err := q.LookupTenantForCellPin(ctx, bridge, tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrCellPinnerTenantNotFound
		}
		return "", fmt.Errorf("cell store: lookup tenant: %w", err)
	}

	cell, err := q.LookupCellForPin(ctx, bridge, cellID)
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
		current, err := q.CountTenantsInCellExcluding(ctx, bridge, generated.CountTenantsInCellExcludingParams{
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

	if err := q.UpdateTenantCellID(ctx, bridge, generated.UpdateTenantCellIDParams{
		CellID: cellID,
		ID:     tenantID,
	}); err != nil {
		return "", fmt.Errorf("cell store: update tenant: %w", err)
	}
	return existingCell, nil
}

// bridge returns the sqlc-compatible DBTX from s.DB.
func (s *PostgresCellStore) bridge() (generated.DBTX, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("cell store: nil DB")
	}
	bridge, ok := s.DB.(SqlcBridge)
	if !ok {
		return nil, fmt.Errorf("cell store: DBTX %T does not implement SqlcBridge", s.DB)
	}
	return bridge.GeneratedDB(), nil
}
