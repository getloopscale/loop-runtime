// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"testing"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

func TestIdentityMigratorNilDB(t *testing.T) {
	// When DB is nil (dev mode), migration should be a no-op
	migrator := auth.NewIdentityMigrator(nil, nil)
	if err := migrator.EnsureMigration(nil, "kratos-id-123", "test@example.com"); err != nil {
		t.Errorf("EnsureMigration with nil DB should not error: %v", err)
	}

	userID, err := migrator.GetLegacyUserID(nil, "kratos-id-123")
	if err != nil {
		t.Errorf("GetLegacyUserID with nil DB should not error: %v", err)
	}
	if userID != "" {
		t.Errorf("GetLegacyUserID with nil DB should return empty string, got %q", userID)
	}
}
