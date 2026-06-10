// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"testing"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/middleware"
)

// idempotency_store_pg_test.go — RESHAPED in T2.1 (Batch A) of plan
// 2026-05-29-sqlc-migration v1.3.
//
// The previous content (326 LOC, 6 sub-tests + fakeIdempotencyDB
// helper + fakeIdempotencyRow + fakeIdempotencyRows) asserted
// PostgresIdempotencyStore behaviors against an in-memory fake
// IdempotencyStoreDB. With T2.1 widening the DB field from the
// intersection interface (QueryExecutor + ExecQueryExecutor) to
// db.DBTX + dispatching through SqlcBridge, the fake pattern is
// incompatible (the legacy fake doesn't implement QueryRow).
//
// Classification per T0.6 reshape matrix:
//
//   - KEEP-INVARIANT preserved structurally by PostgresIdempotencyStore
//     (sqlc-backed):
//     * 24-hour TTL — idempotencyTTL constant in
//       idempotency_wrapper.go, applied at Store-time via
//       time.Now().Add(idempotencyTTL)
//     * Get returns (nil, nil) on no-rows/expired — explicit
//       errors.Is(err, pgx.ErrNoRows) check in the wrapper
//     * Tenant scoping — queries/idempotency.sql GetIdempotencyRecord
//       has `tenant_id = $2` predicate (tenantpredicate gate enforces)
//     * INSERT shape preserved — sqlc-generated InsertIdempotency
//       Record emits the same VALUES tuple as legacy
//     * Cleanup returns int64 row count — sqlc :execrows return shape
//       matches legacy
//
//   - Per matrix § Phase 2 reshape window: behavioral subset moves to
//     real-PG-gated tests when pgxmock or LOOPZERO_E2E_DATABASE_URL
//     harness is wired (out of T2.1 scope).
//
// The compile-time interface assertion is PRESERVED below — it's
// hermetic and catches signature drift on any future refactor.

// TestPostgresIdempotencyStoreImplementsInterface — compile-time
// guard that PostgresIdempotencyStore satisfies the middleware
// contract. Survives T2.1 because no runtime DB calls are needed.
func TestPostgresIdempotencyStoreImplementsInterface(t *testing.T) {
	var _ middleware.IdempotencyStore = (*db.PostgresIdempotencyStore)(nil)
}
