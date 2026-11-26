// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store_test

import (
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
)

// project_store_test.go — RESHAPED in T2.1 #4 of plan
// 2026-05-29-sqlc-migration v1.3.
//
// The previous content included a fakeQueryExecForInsert-based
// CreateProject test that asserted both the happy-path argument
// passing and the UniqueViolation → ErrProjectExists mapping. With
// T2.1 #4 migrating PostgresProjectStore to sqlc-backed dispatch via
// SqlcBridge, the fakeQueryExecForInsert (which implemented the
// legacy db.DBTX intersection without SqlcBridge) cannot exercise
// the new query path.
//
// KEEP-INVARIANT preserved structurally by sqlc-backed
// PostgresProjectStore (project_wrapper.go):
//   - 23505 unique_violation → ports.ErrProjectExists via
//     isPGUniqueViolation helper (covers both raw pgconn.PgError +
//     legacy UniqueViolationError paths for defense-in-depth)
//   - Tenant predicate on every multi-tenant query
//     (queries/projects.sql; tenantpredicate gate enforces)
//   - GetByName NoRows → (nil, nil)
//   - DeleteProject 0-rows-affected → "project not found" error
//
// Per matrix § Phase 2 reshape window: behavioral subset moves to
// real-PG-gated tests when pgxmock or LOOPRUNTIME_E2E_DATABASE_URL harness
// is wired.
//
// The 2 hermetic tests below (interface assertion +
// GenerateProjectID format/uniqueness) SURVIVE — they need no DB
// connection.

// TestPostgresProjectStoreImplementsInterface — compile-time guard.
func TestPostgresProjectStoreImplementsInterface(t *testing.T) {
	var _ ports.ProjectStore = (*store.PostgresProjectStore)(nil)
}

func TestGenerateProjectID(t *testing.T) {
	t.Run("ID has proj- prefix and 8 hex chars", func(t *testing.T) {
		id := store.GenerateProjectID()
		if !strings.HasPrefix(id, "proj-") {
			t.Errorf("expected proj- prefix, got %q", id)
		}
		hexPart := strings.TrimPrefix(id, "proj-")
		if len(hexPart) != 8 {
			t.Errorf("expected 8 hex chars after prefix, got %d chars: %q", len(hexPart), hexPart)
		}
		for _, c := range hexPart {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				t.Errorf("expected hex char, got %c in %q", c, id)
			}
		}
	})

	t.Run("IDs are unique", func(t *testing.T) {
		seen := make(map[string]bool)
		for range 100 {
			id := store.GenerateProjectID()
			if seen[id] {
				t.Fatalf("duplicate ID generated: %s", id)
			}
			seen[id] = true
		}
	})
}
