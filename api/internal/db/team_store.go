// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/loopzero/loopzero/api/internal/routes"
)

// PostgresTeamStore handles team member CRUD operations.
// It implements routes.TeamStore (DIP).
type PostgresTeamStore struct {
	DB DBTX
}

// ListMembers returns all members of a tenant with their roles.
func (s *PostgresTeamStore) ListMembers(ctx context.Context, tenantID string) ([]routes.TeamMemberInfo, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT u.id, u.email, COALESCE(u.display_name, ''), tm.role, tm.created_at
		 FROM tenant_members tm
		 JOIN users u ON tm.user_id = u.id
		 WHERE tm.tenant_id = $1
		 ORDER BY tm.role, COALESCE(u.display_name, u.id)`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("team store: list members failed: %w", err)
	}
	defer rows.Close()

	var members []routes.TeamMemberInfo
	for rows.Next() {
		var userID, email, displayName, role string
		var joinedAt time.Time
		if err := rows.Scan(&userID, &email, &displayName, &role, &joinedAt); err != nil {
			return nil, fmt.Errorf("team store: scan failed: %w", err)
		}
		members = append(members, routes.TeamMemberInfo{
			Username: stripGitHubPrefix(userID),
			Email:    email,
			Role:     role,
			JoinedAt: joinedAt.Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("team store: rows error: %w", err)
	}

	if members == nil {
		members = []routes.TeamMemberInfo{}
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
// Returns routes.ErrMemberNotFound if the member doesn't exist.
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
		return routes.ErrMemberNotFound
	}
	return nil
}

// GetMember returns a single member's info (for authorization checks).
func (s *PostgresTeamStore) GetMember(ctx context.Context, tenantID, userID string) (*routes.TeamMemberInfo, error) {
	row := s.DB.QueryRow(ctx,
		`SELECT u.id, u.email, COALESCE(u.display_name, ''), tm.role, tm.created_at
		 FROM tenant_members tm
		 JOIN users u ON tm.user_id = u.id
		 WHERE tm.tenant_id = $1 AND tm.user_id = $2`,
		tenantID, userID,
	)

	var id, email, displayName, role string
	var joinedAt time.Time
	if err := row.Scan(&id, &email, &displayName, &role, &joinedAt); err != nil {
		if err.Error() == ErrNoRows.Error() {
			return nil, nil
		}
		return nil, fmt.Errorf("team store: get member failed: %w", err)
	}

	return &routes.TeamMemberInfo{
		Username: stripGitHubPrefix(id),
		Email:    email,
		Role:     role,
		JoinedAt: joinedAt.Format(time.RFC3339),
	}, nil
}

// GetUserByUsername finds a user by their GitHub username.
// Returns nil if not found (user hasn't done `loopzero login` yet).
func (s *PostgresTeamStore) GetUserByUsername(ctx context.Context, username string) (*routes.TeamUserInfo, error) {
	// User IDs are "github-{username}" format
	userID := "github-" + username
	row := s.DB.QueryRow(ctx,
		`SELECT id, COALESCE(email, ''), COALESCE(display_name, '') FROM users WHERE id = $1`,
		userID,
	)

	var id, email, displayName string
	if err := row.Scan(&id, &email, &displayName); err != nil {
		if err.Error() == ErrNoRows.Error() {
			return nil, nil
		}
		return nil, fmt.Errorf("team store: get user by username failed: %w", err)
	}

	return &routes.TeamUserInfo{
		ID:          id,
		Email:       email,
		DisplayName: displayName,
	}, nil
}

// stripGitHubPrefix removes the "github-" prefix from user IDs for display.
func stripGitHubPrefix(id string) string {
	return strings.TrimPrefix(id, "github-")
}
