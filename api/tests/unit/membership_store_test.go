// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"testing"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/middleware"
)

func TestPostgresMembershipStoreImplementsInterface(t *testing.T) {
	// Verify that PostgresMembershipStore implements middleware.MembershipStore.
	// This is a compile-time check expressed as a test.
	var _ middleware.MembershipStore = (*db.PostgresMembershipStore)(nil)
}

func TestQueryExecutorInterface(t *testing.T) {
	// Verify that QueryExecutor is properly defined and can be used
	// as a dependency. The actual SQL execution will be tested with
	// integration tests (testcontainers).
	var _ db.QueryExecutor = nil // interface can be nil-assigned
}
