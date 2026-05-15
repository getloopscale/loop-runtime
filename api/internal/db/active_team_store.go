// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"errors"
	"fmt"
)

// PostgresActiveTeamStore persists explicit user-driven active-team selection
// in the users.active_tenant_id column (migration 000018). Plan T2.5 D8.
//
// Semantics:
//   - GetActiveTenant: returns ("", nil) when active_tenant_id is NULL (user
//     never set explicit selection — fall back to JWT middleware heuristic).
//   - SetActiveTenant: UPDATE users SET active_tenant_id = $1 WHERE id = $2.
//     FK enforces tenant exists; ON DELETE SET NULL handles deletion.
//   - ClearActiveTenant: UPDATE users SET active_tenant_id = NULL (EC-4
//     self-heal when /me detects the user is no longer a member).
type PostgresActiveTeamStore struct {
	DB DBTX
}

// NewPostgresActiveTeamStore constructs a PostgresActiveTeamStore.
func NewPostgresActiveTeamStore(db DBTX) *PostgresActiveTeamStore {
	return &PostgresActiveTeamStore{DB: db}
}

// GetActiveTenant returns the user's explicit active_tenant_id, or empty
// string if NULL (legacy default). Errors wrap underlying DB error.
func (s *PostgresActiveTeamStore) GetActiveTenant(ctx context.Context, userID string) (string, error) {
	var active *string
	err := s.DB.QueryRow(ctx,
		"SELECT active_tenant_id FROM users WHERE id = $1",
		userID,
	).Scan(&active)
	if err != nil {
		if errors.Is(err, ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("active team store: query: %w", err)
	}
	if active == nil {
		return "", nil
	}
	return *active, nil
}

// SetActiveTenant updates users.active_tenant_id to the given tenantID.
// The caller is responsible for validating the user is a member of the
// tenant (handler-level enforcement; plan T2.5 D8).
func (s *PostgresActiveTeamStore) SetActiveTenant(ctx context.Context, userID, tenantID string) error {
	_, err := s.DB.Exec(ctx,
		"UPDATE users SET active_tenant_id = $1, updated_at = NOW() WHERE id = $2",
		tenantID, userID,
	)
	if err != nil {
		return fmt.Errorf("active team store: set: %w", err)
	}
	return nil
}

// ClearActiveTenant nulls the active_tenant_id. Used by EC-4 self-heal in
// /me handler when the persisted reference points to a tenant the user is
// no longer a member of (e.g., tenant deleted by admin or membership revoked).
func (s *PostgresActiveTeamStore) ClearActiveTenant(ctx context.Context, userID string) error {
	_, err := s.DB.Exec(ctx,
		"UPDATE users SET active_tenant_id = NULL, updated_at = NOW() WHERE id = $1",
		userID,
	)
	if err != nil {
		return fmt.Errorf("active team store: clear: %w", err)
	}
	return nil
}
