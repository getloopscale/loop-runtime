// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import "github.com/getloopscale/loop-runtime/api/internal/db/dbtx"

// The SQL access port moved to api/internal/db/dbtx so that a consumer needing
// only the transaction handle does not import the entity package to name it.
//
// # These are ALIASES, and that is the whole point
//
// `type DBTX = dbtx.DBTX` is the SAME type under a second name. Every signature
// that says `func(tx db.DBTX) error` — 50 of them across 103 files, including the single
// production implementer at adapters/pgx_adapter.go and every test fake — keeps
// matching without an edit. That is what made the move cost nothing.
//
// Turning any of these into a DEFINITION (`type DBTX dbtx.DBTX`) creates a
// distinct type and breaks all 34 at once. Loud and immediate, by design — but
// if you are reading this because a build broke, that is the cause.
//
// Do not "clean up" this file by deleting it: `db.DBTX` is the name the rest of
// the tree uses, and these aliases are what keep it working.
type (
	// QueryExecutor runs queries returning rows.
	QueryExecutor = dbtx.QueryExecutor
	// Rows is the result of a multi-row query.
	Rows = dbtx.Rows
	// Row is the result of a single-row query.
	Row = dbtx.Row
	// ExecQueryExecutor runs statements returning no rows.
	ExecQueryExecutor = dbtx.ExecQueryExecutor
	// DBTX is the transaction handle: pool or open transaction, indistinctly.
	DBTX = dbtx.DBTX
	// ExecResult holds the result of an exec operation.
	ExecResult = dbtx.ExecResult
	// UniqueViolationError represents a PostgreSQL unique constraint violation.
	UniqueViolationError = dbtx.UniqueViolationError
)

// Sentinel errors live in the port package too — a caller comparing against
// ErrNoRows is doing SQL access, not touching an entity. Aliased as VARIABLES
// (not copied) so `errors.Is` keeps matching across both names.
var (
	// ErrNoRows is returned when a single-row query matched nothing.
	ErrNoRows = dbtx.ErrNoRows
	// ErrNilTxStatusMutation guards the advisory-lock scope on status writes.
	ErrNilTxStatusMutation = dbtx.ErrNilTxStatusMutation
)
