// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"
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
