// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
)

// IdentityMigrator handles lazy migration of existing users to Kratos identities.
// When a user authenticates via social login (GitHub/Google) and already exists
// in the legacy `users` table, this maps their Kratos identity ID to the legacy user.
//
// DEPRECATED: This table and migrator will be removed after all legacy users are migrated.
// Target removal: 2026-Q3. Once no rows in user_identity_map reference users that
// authenticate only via the legacy GitHub flow, this can be dropped.
// §5.2: users.id IS the Kratos UUID for all new users. This bridge is transitional.
type IdentityMigrator struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewIdentityMigrator creates a migrator that bridges Kratos identities with legacy users.
func NewIdentityMigrator(db *sql.DB, logger *slog.Logger) *IdentityMigrator {
	return &IdentityMigrator{db: db, logger: logger}
}

// EnsureMigration checks if a Kratos identity has a corresponding legacy user.
// If the user exists (matched by email), it creates a mapping.
// If no legacy user exists, this is a new signup — no migration needed.
//
// This is called after every successful authentication via Kratos.
func (m *IdentityMigrator) EnsureMigration(ctx context.Context, kratosIdentityID, email string) error {
	if m.db == nil {
		return nil // DB not configured (dev mode without DB)
	}

	// Check if mapping already exists
	var exists bool
	err := m.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM user_identity_map WHERE kratos_identity_id = $1)",
		kratosIdentityID,
	).Scan(&exists)
	if err != nil {
		// Table may not exist yet — that's OK, migration hasn't been applied
		if isTableNotFoundError(err) {
			m.logger.Debug("user_identity_map table does not exist yet, skipping migration")
			return nil
		}
		return fmt.Errorf("check identity mapping: %w", err)
	}
	if exists {
		return nil // Already mapped
	}

	// Find legacy user by email
	var legacyUserID string
	err = m.db.QueryRowContext(ctx,
		"SELECT id FROM users WHERE email = $1",
		email,
	).Scan(&legacyUserID)
	if err == sql.ErrNoRows {
		// No legacy user — this is a new signup, no migration needed
		return nil
	}
	if err != nil {
		return fmt.Errorf("lookup legacy user: %w", err)
	}

	// Create mapping
	_, err = m.db.ExecContext(ctx,
		"INSERT INTO user_identity_map (user_id, kratos_identity_id, created_at) VALUES ($1, $2, now()) ON CONFLICT DO NOTHING",
		legacyUserID, kratosIdentityID,
	)
	if err != nil {
		return fmt.Errorf("create identity mapping: %w", err)
	}

	m.logger.Info("legacy user migrated to Kratos identity",
		"legacy_user_id", legacyUserID,
		"kratos_identity_id", kratosIdentityID,
		"email", email,
	)
	return nil
}

// GetLegacyUserID returns the legacy user ID for a Kratos identity, if mapped.
func (m *IdentityMigrator) GetLegacyUserID(ctx context.Context, kratosIdentityID string) (string, error) {
	if m.db == nil {
		return "", nil
	}

	var userID string
	err := m.db.QueryRowContext(ctx,
		"SELECT user_id FROM user_identity_map WHERE kratos_identity_id = $1",
		kratosIdentityID,
	).Scan(&userID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		if isTableNotFoundError(err) {
			return "", nil
		}
		return "", fmt.Errorf("get legacy user: %w", err)
	}
	return userID, nil
}

func isTableNotFoundError(err error) bool {
	return strings.Contains(err.Error(), "does not exist") ||
		strings.Contains(err.Error(), "42P01")
}
