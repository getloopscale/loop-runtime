// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// membership_wrapper.go — sqlc-backed implementation of
// PostgresMembershipStore.GetMemberships. T2.1 Batch A #8a of plan
// docs/plans/2026-05-29-sqlc-migration-plan.md v1.3.
//
// The 1 prod call site (api/cmd/setup_database.go:67) compiles
// UNCHANGED — struct-literal `&db.PostgresMembershipStore{DB:
// poolCfg}` preserved (struct in membership_types.go).
//
// Invariants preserved:
//   - Returns ALL tenant memberships for the user (JWT-
//     authenticated principal). Cross-tenant by design.
//   - Empty result → nil slice (legacy contract — caller uses
//     len() not nil check, so nil is acceptable).

import (
	"context"
	"fmt"

	"github.com/getloopscale/loopzero/api/internal/db"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
)

// GetMemberships returns all tenant memberships for a given user ID.
func (s *PostgresMembershipStore) GetMemberships(ctx context.Context, userID string) ([]auth.TenantMembership, error) {
	bridge, ok := s.DB.(db.SqlcBridge)
	if !ok {
		return nil, fmt.Errorf("membership store: DBTX %T does not implement SqlcBridge", s.DB)
	}
	q := generated.New()

	rows, err := q.GetTenantMembershipsByUser(ctx, bridge.GeneratedDB(), userID)
	if err != nil {
		return nil, fmt.Errorf("membership store: query failed: %w", err)
	}
	out := make([]auth.TenantMembership, 0, len(rows))
	for _, r := range rows {
		out = append(out, auth.TenantMembership{
			TenantID: r.TenantID,
			Role:     r.Role,
		})
	}
	return out, nil
}
