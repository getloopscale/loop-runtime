// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PostgresCellStore provides Cell-related database queries.
// ADR-0008 Phase 0: CellTenantCounts for metrics.
// ADR-0008 Phase 1: CountByCell and GetTenantCellID for PlacementEngine.
type PostgresCellStore struct {
	DB DBTX
}

// CellTenantCounts returns the number of tenants per cell_id.
// Used by the CellMetricsCollector to emit loopzero_cell_tenant_count gauge.
func (s *PostgresCellStore) CellTenantCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT cell_id, COUNT(*) FROM tenants GROUP BY cell_id`)
	if err != nil {
		return nil, fmt.Errorf("cell store: query tenant counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var cellID string
		var count int
		if err := rows.Scan(&cellID, &count); err != nil {
			return nil, fmt.Errorf("cell store: scan: %w", err)
		}
		counts[cellID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cell store: rows error: %w", err)
	}

	return counts, nil
}

// CountByCell is an alias for CellTenantCounts that satisfies
// the placement.TenantCellQuerier interface.
func (s *PostgresCellStore) CountByCell(ctx context.Context) (map[string]int, error) {
	return s.CellTenantCounts(ctx)
}

// GetTenantCellID returns the cell_id for a specific tenant.
// Returns error if the tenant does not exist.
func (s *PostgresCellStore) GetTenantCellID(ctx context.Context, tenantID string) (string, error) {
	var cellID string
	err := s.DB.QueryRow(ctx,
		`SELECT cell_id FROM tenants WHERE id = $1`, tenantID).Scan(&cellID)
	if err != nil {
		return "", fmt.Errorf("cell store: get tenant cell_id: %w", err)
	}
	return cellID, nil
}

// Sentinel errors mirroring services.* so the cell_store doesn't import
// services (would cycle). The HTTP handler in admin_pin_cell.go matches
// against the strings, so the pinner service-side and db-side strings
// must stay in sync (test pinned in services/cell_pinner_test.go).
var (
	ErrCellPinnerTenantNotFound = errors.New("tenant not found")
	ErrCellPinnerCellNotFound   = errors.New("cell not found")
	ErrCellPinnerNotPinnable    = errors.New("cell not pinnable")
	ErrCellPinnerAtCapacity     = errors.New("cell at capacity")
)

// PinTenantToCell implements services.CellPinnerStore. SERIALIZABLE
// transaction performs:
//   1. validate tenant exists → ErrTenantNotFound
//   2. validate cell exists → ErrCellNotFound
//   3. validate cell.status == 'active' → ErrCellNotPinnable
//   4. capacity check (NULL = unlimited; current excludes moving tenant)
//      → ErrCellAtCapacity
//   5. UPDATE tenants.cell_id, RETURNING previous cell_id
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
	var existingCell string
	if err := s.DB.QueryRow(ctx, `SELECT cell_id FROM tenants WHERE id = $1`, tenantID).Scan(&existingCell); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrCellPinnerTenantNotFound
		}
		return "", fmt.Errorf("cell store: lookup tenant: %w", err)
	}
	var status string
	var capacity *int
	if err := s.DB.QueryRow(ctx,
		`SELECT status, capacity_tenants_max FROM cells WHERE cell_id = $1`, cellID).
		Scan(&status, &capacity); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrCellPinnerCellNotFound
		}
		return "", fmt.Errorf("cell store: lookup cell: %w", err)
	}
	if status != "active" {
		return "", ErrCellPinnerNotPinnable
	}
	if capacity != nil {
		var currentCount int
		if err := s.DB.QueryRow(ctx,
			`SELECT COUNT(*) FROM tenants WHERE cell_id = $1 AND id <> $2`,
			cellID, tenantID).Scan(&currentCount); err != nil {
			return "", fmt.Errorf("cell store: count target cell: %w", err)
		}
		if currentCount >= *capacity {
			return "", ErrCellPinnerAtCapacity
		}
	}
	if _, err := s.DB.Exec(ctx,
		`UPDATE tenants SET cell_id = $1 WHERE id = $2`, cellID, tenantID); err != nil {
		return "", fmt.Errorf("cell store: update tenant: %w", err)
	}
	return existingCell, nil
}
