// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package db holds the persistence primitives every store is built on: the pgx
// pool and its lifecycle, the DBTX abstraction that lets a store take either a
// pool or a transaction, and the bridge to the pgx-native handle that
// sqlc-generated queries require.
//
// It deliberately holds no query and no business rule. A store that knows what a
// build is belongs in internal/db/store; what lives here is what would be true of
// any Postgres-backed system.
package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
)

// SqlcBridge is the bridge interface from the internal db.DBTX abstraction to
// the pgx-native generated.DBTX expected by sqlc-generated query methods.
// Concrete adapters that wrap a real *pgxpool.Pool or pgx.Tx implement this
// by exposing the underlying pgx-native handle. Test fakes that satisfy
// db.DBTX without holding a real pgx connection do NOT implement this — by
// design, sqlc-backed stores require pgxmock or a real PG instance for tests
// (per T0.6 reshape matrix of plan 2026-05-29-sqlc-migration v1.3).
type SqlcBridge interface {
	GeneratedDB() generated.DBTX
}

// PgxAdapter wraps a pgxpool.Pool to implement QueryExecutor and ExecQueryExecutor.
// This adapter translates pgx-specific types to our domain interfaces,
// keeping business logic free from pgx imports (DIP).
type PgxAdapter struct {
	Pool *pgxpool.Pool
}

// Query executes a query and returns Rows.
func (a *PgxAdapter) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	rows, err := a.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return &pgxRows{rows: rows}, nil
}

// Exec executes a statement and returns ExecResult.
func (a *PgxAdapter) Exec(ctx context.Context, sql string, args ...any) (ExecResult, error) {
	tag, err := a.Pool.Exec(ctx, sql, args...)
	if err != nil {
		return ExecResult{}, translatePgError(err)
	}
	return ExecResult{RowsAffected: tag.RowsAffected()}, nil
}

// QueryRow executes a query that returns at most one row.
func (a *PgxAdapter) QueryRow(ctx context.Context, sql string, args ...any) Row {
	return &pgxRow{row: a.Pool.QueryRow(ctx, sql, args...)}
}

// Ping checks database connectivity. Satisfies routes.DBHealthChecker.
func (a *PgxAdapter) Ping() error {
	return a.Pool.Ping(context.Background())
}

// Close closes the underlying pool.
func (a *PgxAdapter) Close() {
	a.Pool.Close()
}

// GeneratedDB returns the underlying pgxpool.Pool as a generated.DBTX
// (sqlc-emitted pgx-native interface). Bridges PgxAdapter into sqlc-backed
// stores per T1.1c2b of plan 2026-05-29-sqlc-migration v1.3.
func (a *PgxAdapter) GeneratedDB() generated.DBTX {
	return a.Pool
}

// PgxTxAdapter wraps a pgx.Tx to implement DBTX.
// MEET-053 D-01: Used by the placement serializer to inject a transaction
// into the bootstrap store, fixing the TOCTOU race between AssignCell and
// the tenant INSERT.
type PgxTxAdapter struct {
	Tx pgx.Tx
}

func (a *PgxTxAdapter) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	rows, err := a.Tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return &pgxRows{rows: rows}, nil
}

func (a *PgxTxAdapter) Exec(ctx context.Context, sql string, args ...any) (ExecResult, error) {
	tag, err := a.Tx.Exec(ctx, sql, args...)
	if err != nil {
		return ExecResult{}, translatePgError(err)
	}
	return ExecResult{RowsAffected: tag.RowsAffected()}, nil
}

func (a *PgxTxAdapter) QueryRow(ctx context.Context, sql string, args ...any) Row {
	return &pgxRow{row: a.Tx.QueryRow(ctx, sql, args...)}
}

// GeneratedDB returns the underlying pgx.Tx as a generated.DBTX (sqlc-
// emitted pgx-native interface). Bridges PgxTxAdapter into sqlc-backed
// stores per T1.1c2b of plan 2026-05-29-sqlc-migration v1.3.
func (a *PgxTxAdapter) GeneratedDB() generated.DBTX {
	return a.Tx
}

// Compile-time assertion that BOTH PgxAdapter and PgxTxAdapter satisfy
// db.DBTX AND db.SqlcBridge. PgxTxAdapter is the only pgx.Tx bridge: the
// environment-lock path (adapters.PoolEnvironmentLocker) and the placement
// path both wrap their transaction in it. There used to be a second, lock-side
// copy that had to be kept "in lockstep" with this one; it drifted twice
// (first missing SqlcBridge, then missing translatePgError) and was deleted.
var _ DBTX = (*PgxAdapter)(nil)
var _ SqlcBridge = (*PgxAdapter)(nil)
var _ DBTX = (*PgxTxAdapter)(nil)
var _ SqlcBridge = (*PgxTxAdapter)(nil)

// pgxRows wraps pgx.Rows to implement our Rows interface.
type pgxRows struct {
	rows pgx.Rows
}

func (r *pgxRows) Next() bool             { return r.rows.Next() }
func (r *pgxRows) Scan(dest ...any) error { return r.rows.Scan(dest...) }
func (r *pgxRows) Err() error             { return r.rows.Err() }
func (r *pgxRows) Close()                 { r.rows.Close() }

// pgxRow wraps pgx.Row to implement our Row interface.
type pgxRow struct {
	row pgx.Row
}

func (r *pgxRow) Scan(dest ...any) error {
	err := r.row.Scan(dest...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoRows
		}
		return translatePgError(err)
	}
	return nil
}

// translatePgError converts pgx errors to our domain error types.
func translatePgError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 23505 = unique_violation
		if pgErr.Code == "23505" {
			return &UniqueViolationError{Constraint: pgErr.ConstraintName}
		}
	}
	return err
}
