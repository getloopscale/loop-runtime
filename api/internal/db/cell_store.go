// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"
)

// PostgresCellStore provides Cell-related database queries.
// ADR-0008 Phase 0: only CellTenantCounts is needed.
type PostgresCellStore struct {
	DB QueryExecutor
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
