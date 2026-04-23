// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"
	"time"

	"github.com/loopzero/loopzero/api/internal/domain"
)

// PostgresTeamStore handles team member CRUD operations.
// It implements domain.TeamStore (DIP).
type PostgresTeamStore struct {
	DB DBTX
}

// ListMembers returns all members of a tenant with their roles.
// AUDIT-7: Username comes from the `users.username` column (provider-agnostic),
// not from stripping a "github-" prefix off the id.
func (s *PostgresTeamStore) ListMembers(ctx context.Context, tenantID string) ([]domain.TeamMemberInfo, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT u.username, u.email, tm.role, tm.created_at
		 FROM tenant_members tm
		 JOIN users u ON tm.user_id = u.id
		 WHERE tm.tenant_id = $1
		 ORDER BY tm.role, u.username`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("team store: list members failed: %w", err)
	}
	defer rows.Close()

	var members []domain.TeamMemberInfo
	for rows.Next() {
		var username, email, role string
		var joinedAt time.Time
		if err := rows.Scan(&username, &email, &role, &joinedAt); err != nil {
			return nil, fmt.Errorf("team store: scan failed: %w", err)
		}
		members = append(members, domain.TeamMemberInfo{
			Username: username,
			Email:    email,
			Role:     role,
			JoinedAt: joinedAt.Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("team store: rows error: %w", err)
	}

	if members == nil {
		members = []domain.TeamMemberInfo{}
	}
	return members, nil
}

// AddMember adds a user to a tenant with the given role.
// Idempotent: if already a member, returns without error.
func (s *PostgresTeamStore) AddMember(ctx context.Context, tenantID, userID, role string) error {
	_, err := s.DB.Exec(ctx,
		`INSERT INTO tenant_members (tenant_id, user_id, role)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (tenant_id, user_id) DO NOTHING`,
		tenantID, userID, role,
	)
	if err != nil {
		return fmt.Errorf("team store: add member failed: %w", err)
	}
	return nil
}

// RemoveMember removes a user from a tenant.
// Idempotent: if not a member, returns without error.
func (s *PostgresTeamStore) RemoveMember(ctx context.Context, tenantID, userID string) error {
	_, err := s.DB.Exec(ctx,
		`DELETE FROM tenant_members WHERE tenant_id = $1 AND user_id = $2`,
		tenantID, userID,
	)
	if err != nil {
		return fmt.Errorf("team store: remove member failed: %w", err)
	}
	return nil
}

// UpdateMemberRole changes a member's role.
// Returns domain.ErrMemberNotFound if the member doesn't exist.
func (s *PostgresTeamStore) UpdateMemberRole(ctx context.Context, tenantID, userID, newRole string) error {
	result, err := s.DB.Exec(ctx,
		`UPDATE tenant_members SET role = $1
		 WHERE tenant_id = $2 AND user_id = $3`,
		newRole, tenantID, userID,
	)
	if err != nil {
		return fmt.Errorf("team store: update role failed: %w", err)
	}
	if result.RowsAffected == 0 {
		return domain.ErrMemberNotFound
	}
	return nil
}

// GetMember returns a single member's info (for authorization checks).
// AUDIT-7: Username from the users.username column, provider-agnostic.
func (s *PostgresTeamStore) GetMember(ctx context.Context, tenantID, userID string) (*domain.TeamMemberInfo, error) {
	row := s.DB.QueryRow(ctx,
		`SELECT u.username, u.email, tm.role, tm.created_at
		 FROM tenant_members tm
		 JOIN users u ON tm.user_id = u.id
		 WHERE tm.tenant_id = $1 AND tm.user_id = $2`,
		tenantID, userID,
	)

	var username, email, role string
	var joinedAt time.Time
	if err := row.Scan(&username, &email, &role, &joinedAt); err != nil {
		if err.Error() == ErrNoRows.Error() {
			return nil, nil
		}
		return nil, fmt.Errorf("team store: get member failed: %w", err)
	}

	return &domain.TeamMemberInfo{
		Username: username,
		Email:    email,
		Role:     role,
		JoinedAt: joinedAt.Format(time.RFC3339),
	}, nil
}

// GetUsernameByID returns the display-friendly username for an opaque user_id.
// Returns "" (no error) if the user is not found. AUDIT-10: used by /whoami
// to show a meaningful identity when the token has no email claim.
func (s *PostgresTeamStore) GetUsernameByID(ctx context.Context, userID string) (string, error) {
	row := s.DB.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, userID)
	var username string
	if err := row.Scan(&username); err != nil {
		if err.Error() == ErrNoRows.Error() {
			return "", nil
		}
		return "", fmt.Errorf("team store: get username by id failed: %w", err)
	}
	return username, nil
}

// GetUserByUsername finds a user by their username (provider-agnostic).
// Returns nil if not found. AUDIT-7: no longer assumes "github-" id prefix;
// queries the `users.username` column populated by migration 000037.
func (s *PostgresTeamStore) GetUserByUsername(ctx context.Context, username string) (*domain.TeamUserInfo, error) {
	row := s.DB.QueryRow(ctx,
		`SELECT id, COALESCE(email, ''), COALESCE(display_name, '') FROM users WHERE username = $1`,
		username,
	)

	var id, email, displayName string
	if err := row.Scan(&id, &email, &displayName); err != nil {
		if err.Error() == ErrNoRows.Error() {
			return nil, nil
		}
		return nil, fmt.Errorf("team store: get user by username failed: %w", err)
	}

	return &domain.TeamUserInfo{
		ID:          id,
		Email:       email,
		DisplayName: displayName,
	}, nil
}
