// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"
)

// PostgresTenantStore provides direct tenant-level queries used by admin
// primitives such as bulk eviction (T-02 in docs/plans/50k-immediate-focus.md).
//
// Unlike the high-traffic stores (build, release, project), this store is only
// exercised by operator-level endpoints that are feature-gated behind
// LOOPZERO_ADMIN_API. Queries here intentionally bypass tenant scoping — the caller
// is already an operator and needs to read/write across tenants.
type PostgresTenantStore struct {
	DB DBTX
}

// ListTenantIDsByPattern returns tenant IDs matching a SQL LIKE pattern.
//
// Used by the AdminEvictHandler to resolve patterns like "tn-lt-l3-%" into a
// concrete set of IDs before enqueueing eviction. The pattern is applied
// directly to the tenants.id column with ESCAPE '\' to allow literal _/%
// characters if the caller needs them.
//
// Security: the caller is responsible for rejecting dangerous free-text
// patterns ('%', 'tn-%') BEFORE calling this — see admin_evict.go validation.
// This method does NOT validate the pattern; it trusts the handler layer.
//
// Results are sorted ASC for determinism. Limited to 10_000 rows to prevent
// pathological patterns from materializing the entire tenants table into
// memory. Patterns that need more than 10_000 matches should be split.
func (s *PostgresTenantStore) ListTenantIDsByPattern(ctx context.Context, pattern string) ([]string, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT id FROM tenants WHERE id LIKE $1 ESCAPE '\' ORDER BY id ASC LIMIT 10000`,
		pattern,
	)
	if err != nil {
		return nil, fmt.Errorf("tenant store: list by pattern: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("tenant store: scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tenant store: rows error: %w", err)
	}
	return ids, nil
}
