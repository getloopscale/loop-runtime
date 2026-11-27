// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package teams is the team-membership feature: who belongs to a tenant and in
// which role. Its handlers live in teamshttp; the membership records themselves
// are persisted by the shared db/store.
package teams

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// TeamStore abstracts team member persistence (DIP).
type TeamStore interface {
	ListMembers(ctx context.Context, tenantID string) ([]ports.TeamMemberInfo, error)
	AddMember(ctx context.Context, tenantID, userID, role string) error
	RemoveMember(ctx context.Context, tenantID, userID string) error
	UpdateMemberRole(ctx context.Context, tenantID, userID, newRole string) error
	GetMember(ctx context.Context, tenantID, userID string) (*ports.TeamMemberInfo, error)
	GetUserByUsername(ctx context.Context, username string) (*ports.TeamUserInfo, error)
	// GetUsernameByID returns the user's display-friendly username for the given
	// opaque user_id. AUDIT-10: lets `loopscale whoami` show a meaningful identity
	// even when the token has no email claim (OAuth2 client_credentials).
	// Returns empty string if user not found.
	GetUsernameByID(ctx context.Context, userID string) (string, error)
}
