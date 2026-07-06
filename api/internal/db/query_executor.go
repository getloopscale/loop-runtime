// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"errors"
)

// ErrNoRows is returned when a query expected a single row but found none.
// Equivalent to pgx.ErrNoRows / sql.ErrNoRows.
var ErrNoRows = errors.New("no rows in result set")

// ErrNilTxStatusMutation is returned by the read-modify-write status mutators
// (BuildStoreSqlc / ReleaseStoreSqlc UpdateStatus) when called with a nil tx.
// A status read-modify-write (GetByIDTx → SafeTransition → UPDATE) MUST run in
// one transaction under withEnvironmentLock; a nil tx would split the fetch and
// the update across two pooled auto-committed connections (no atomicity, the
// advisory lock never held). Lock-free conditional updates must use the
// single-statement CAS UpdateStatusIfCurrent instead. M9 5b, ADR
// 2026-07-03-m9-5b-nil-tx-status-atomicity.
var ErrNilTxStatusMutation = errors.New("db: status read-modify-write requires a non-nil tx (advisory-lock scope) — use UpdateStatusIfCurrent for lock-free conditional updates")

// QueryExecutor abstracts pgx query methods for read operations.
// Implemented by *pgxpool.Pool, *pgx.Conn, and test fakes.
type QueryExecutor interface {
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
}

// Rows abstracts pgx.Rows for testability.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}

// Row abstracts a single-row query result for testability.
type Row interface {
	Scan(dest ...any) error
}

// ExecQueryExecutor abstracts pgx exec methods for write operations.
// Implemented by *pgxpool.Pool, *pgx.Conn, and test fakes.
type ExecQueryExecutor interface {
	Exec(ctx context.Context, sql string, args ...any) (ExecResult, error)
}

// DBTX combines Query, QueryRow, and Exec operations.
// Implemented by both connection pools and transactions, enabling
// stores to work transparently with either.
type DBTX interface {
	QueryExecutor
	ExecQueryExecutor
	QueryRow(ctx context.Context, sql string, args ...any) Row
}

// ExecResult holds the result of an exec operation.
type ExecResult struct {
	RowsAffected int64
}

// UniqueViolationError represents a PostgreSQL unique constraint violation (23505).
// Used to detect duplicate key errors without importing pgx in business logic.
type UniqueViolationError struct {
	Constraint string
}

func (e *UniqueViolationError) Error() string {
	return "unique violation: " + e.Constraint
}
