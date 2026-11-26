// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import "github.com/getloopscale/loop-runtime/api/internal/db"

// idempotency_types.go — public types for the idempotency_keys
// domain that survive the T2.1 (Batch A) deletion of the legacy
// hand-rolled `idempotency_store.go`. The 3 prod call sites continue
// to use the struct-literal `&db.PostgresIdempotencyStore{DB:
// dbAdapter}` shape; only the methods (now in
// idempotency_wrapper.go) switch to sqlc-generated calls.

// PostgresIdempotencyStore implements middleware.IdempotencyStore
// backed by PostgreSQL via sqlc. Uses the idempotency_keys table
// from migration 000001. TTL is 24 hours per spec §4.3.
//
// T2.1 Batch A (plan 2026-05-29-sqlc-migration v1.3): DB field type
// widened from the local IdempotencyStoreDB intersection
// (QueryExecutor + ExecQueryExecutor) to the standard db.DBTX
// interface so the sqlc-backed methods can dispatch through
// SqlcBridge. All 3 prod call sites pass *PgxAdapter which satisfies
// db.DBTX.
type PostgresIdempotencyStore struct {
	DB db.DBTX
}
