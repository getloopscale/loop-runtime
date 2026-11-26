// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// active_team_wrapper.go — sqlc-backed implementation of
// PostgresActiveTeamStore. T2.1 Batch A (2nd of 10 stores) of plan
// 2026-05-29-sqlc-migration-plan v1.3.
//
// The 1 prod call site (api/cmd/setup_server.go:220) uses the
// constructor `db.NewPostgresActiveTeamStore(dbAdapter)` — preserved
// verbatim (constructor + struct live in active_team_types.go).
//
// Invariants preserved:
//   - GetActiveTenant returns ("", nil) for NULL active_tenant_id +
//     for missing user (legacy fallback to JWT heuristic)
//   - SetActiveTenant updates updated_at = NOW() to drive cache
//     invalidation downstream
//   - ClearActiveTenant nulls active_tenant_id (EC-4 self-heal)

import (
	"context"
	"errors"
	"fmt"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/dbconv"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
)

// GetActiveTenant returns the user's explicit active_tenant_id, or
// empty string when NULL or user not found (legacy default).
func (s *PostgresActiveTeamStore) GetActiveTenant(ctx context.Context, userID string) (string, error) {
	gdb := db.Generated(s.DB)
	q := generated.New()

	active, err := q.GetActiveTenantForUser(ctx, gdb, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("active team store: query: %w", err)
	}
	if active == nil {
		return "", nil
	}
	return *active, nil
}

// SetActiveTenant updates users.active_tenant_id. Caller validates
// the user is a member of the tenant before invoking (handler-level).
func (s *PostgresActiveTeamStore) SetActiveTenant(ctx context.Context, userID, tenantID string) error {
	gdb := db.Generated(s.DB)
	q := generated.New()

	if err := q.SetActiveTenantForUser(ctx, gdb, generated.SetActiveTenantForUserParams{
		ActiveTenantID: dbconv.StringEmptyToNilPtr(tenantID),
		ID:             userID,
	}); err != nil {
		return fmt.Errorf("active team store: set: %w", err)
	}
	return nil
}

// ClearActiveTenant nulls the user's active_tenant_id (EC-4 self-heal
// when /me detects membership revocation).
func (s *PostgresActiveTeamStore) ClearActiveTenant(ctx context.Context, userID string) error {
	gdb := db.Generated(s.DB)
	q := generated.New()

	if err := q.ClearActiveTenantForUser(ctx, gdb, userID); err != nil {
		return fmt.Errorf("active team store: clear: %w", err)
	}
	return nil
}
