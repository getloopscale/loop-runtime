// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

// team_wrapper.go — sqlc-backed implementation of
// PostgresTeamStore. T2.1 Batch A #5 of plan
// 2026-05-29-sqlc-migration-plan v1.3.
//
// The 1 prod call site (api/cmd/setup_server.go:215) compiles
// UNCHANGED — struct-literal `&db.PostgresTeamStore{DB: dbAdapter}`
// preserved (struct in team_types.go; methods here).
//
// Invariants preserved:
//   - AddMember idempotent (ON CONFLICT DO NOTHING)
//   - RemoveMember idempotent (silent on missing rows)
//   - UpdateMemberRole 0-rows-affected → ports.ErrMemberNotFound
//   - GetMember / GetUsernameByID / GetUserByUsername NoRows →
//     (nil, nil) / "" / (nil, nil) per legacy contract
//   - AUDIT-7 username from users.username (JOIN-based, provider-
//     agnostic) preserved in queries/teams.sql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// ListMembers returns all members of a tenant with their roles.
func (s *PostgresTeamStore) ListMembers(ctx context.Context, tenantID string) ([]ports.TeamMemberInfo, error) {
	gdb := db.Generated(s.DB)
	q := generated.New()

	rows, err := q.ListTenantMembers(ctx, gdb, tenantID)
	if err != nil {
		return nil, fmt.Errorf("team store: list members failed: %w", err)
	}
	members := make([]ports.TeamMemberInfo, 0, len(rows))
	for _, r := range rows {
		info := ports.TeamMemberInfo{
			Username: r.Username,
			Email:    r.Email,
			Role:     r.Role,
		}
		if r.CreatedAt.Valid {
			info.JoinedAt = r.CreatedAt.Time.Format(time.RFC3339)
		}
		members = append(members, info)
	}
	return members, nil
}

// AddMember adds a user to a tenant with the given role. Idempotent.
func (s *PostgresTeamStore) AddMember(ctx context.Context, tenantID, userID, role string) error {
	gdb := db.Generated(s.DB)
	q := generated.New()

	if err := q.AddTenantMember(ctx, gdb, generated.AddTenantMemberParams{
		TenantID: tenantID,
		UserID:   userID,
		Role:     role,
	}); err != nil {
		return fmt.Errorf("team store: add member failed: %w", err)
	}
	return nil
}

// RemoveMember removes a user from a tenant. Idempotent.
func (s *PostgresTeamStore) RemoveMember(ctx context.Context, tenantID, userID string) error {
	gdb := db.Generated(s.DB)
	q := generated.New()

	if err := q.RemoveTenantMember(ctx, gdb, generated.RemoveTenantMemberParams{
		TenantID: tenantID,
		UserID:   userID,
	}); err != nil {
		return fmt.Errorf("team store: remove member failed: %w", err)
	}
	return nil
}

// UpdateMemberRole changes a member's role. Returns ports.
// ErrMemberNotFound when the row doesn't exist.
func (s *PostgresTeamStore) UpdateMemberRole(ctx context.Context, tenantID, userID, newRole string) error {
	gdb := db.Generated(s.DB)
	q := generated.New()

	rows, err := q.UpdateTenantMemberRole(ctx, gdb, generated.UpdateTenantMemberRoleParams{
		Role:     newRole,
		TenantID: tenantID,
		UserID:   userID,
	})
	if err != nil {
		return fmt.Errorf("team store: update role failed: %w", err)
	}
	if rows == 0 {
		return ports.ErrMemberNotFound
	}
	return nil
}

// GetMember returns a single member's info. NoRows → (nil, nil).
func (s *PostgresTeamStore) GetMember(ctx context.Context, tenantID, userID string) (*ports.TeamMemberInfo, error) {
	gdb := db.Generated(s.DB)
	q := generated.New()

	row, err := q.GetTenantMember(ctx, gdb, generated.GetTenantMemberParams{
		TenantID: tenantID,
		UserID:   userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("team store: get member failed: %w", err)
	}
	info := &ports.TeamMemberInfo{
		Username: row.Username,
		Email:    row.Email,
		Role:     row.Role,
	}
	if row.CreatedAt.Valid {
		info.JoinedAt = row.CreatedAt.Time.Format(time.RFC3339)
	}
	return info, nil
}

// GetUsernameByID returns the display-friendly username for an
// opaque user_id. Returns "" (no error) if user not found.
func (s *PostgresTeamStore) GetUsernameByID(ctx context.Context, userID string) (string, error) {
	gdb := db.Generated(s.DB)
	q := generated.New()

	username, err := q.GetUsernameByUserID(ctx, gdb, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("team store: get username by id failed: %w", err)
	}
	return username, nil
}

// GetUserByUsername finds a user by their username. NoRows → (nil,
// nil).
func (s *PostgresTeamStore) GetUserByUsername(ctx context.Context, username string) (*ports.TeamUserInfo, error) {
	gdb := db.Generated(s.DB)
	q := generated.New()

	row, err := q.GetUserByUsername(ctx, gdb, username)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("team store: get user by username failed: %w", err)
	}
	return &ports.TeamUserInfo{
		ID:          row.ID,
		Email:       row.Email,
		DisplayName: row.DisplayName,
	}, nil
}
