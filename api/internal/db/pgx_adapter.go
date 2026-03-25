// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

// pgxRows wraps pgx.Rows to implement our Rows interface.
type pgxRows struct {
	rows pgx.Rows
}

func (r *pgxRows) Next() bool        { return r.rows.Next() }
func (r *pgxRows) Scan(dest ...any) error { return r.rows.Scan(dest...) }
func (r *pgxRows) Err() error         { return r.rows.Err() }
func (r *pgxRows) Close()             { r.rows.Close() }

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
