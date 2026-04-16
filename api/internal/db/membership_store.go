// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"

	"github.com/loopzero/loopzero/api/internal/middleware"
)

// PostgresMembershipStore retrieves tenant memberships from PostgreSQL.
// It implements middleware.MembershipStore (DIP).
type PostgresMembershipStore struct {
	DB QueryExecutor
}

// GetMemberships returns all tenant memberships for a given user ID.
func (s *PostgresMembershipStore) GetMemberships(ctx context.Context, userID string) ([]middleware.TenantMembership, error) {
	rows, err := s.DB.Query(ctx,
		"SELECT tenant_id, role FROM tenant_members WHERE user_id = $1",
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("membership store: query failed: %w", err)
	}
	defer rows.Close()

	var memberships []middleware.TenantMembership
	for rows.Next() {
		var m middleware.TenantMembership
		if err := rows.Scan(&m.TenantID, &m.Role); err != nil {
			return nil, fmt.Errorf("membership store: scan failed: %w", err)
		}
		memberships = append(memberships, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("membership store: rows error: %w", err)
	}

	return memberships, nil
}
