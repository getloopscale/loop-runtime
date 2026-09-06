// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/db"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
)

func TestPostgresMembershipStoreImplementsInterface(t *testing.T) {
	// Verify that PostgresMembershipStore implements auth.MembershipStore.
	// This is a compile-time check expressed as a test.
	var _ auth.MembershipStore = (*store.PostgresMembershipStore)(nil)
}

func TestQueryExecutorInterface(t *testing.T) {
	// Verify that QueryExecutor is properly defined and can be used
	// as a dependency. The actual SQL execution will be tested with
	// integration tests (testcontainers).
	var _ db.QueryExecutor = nil // interface can be nil-assigned
}
