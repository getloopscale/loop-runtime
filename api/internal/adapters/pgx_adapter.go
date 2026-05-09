// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/lock"
)

// PgxTxDBTX adapts a pgx.Tx to the db.DBTX interface.
// This bridges the lock package (which uses pgx.Tx) with the store layer
// (which uses db.DBTX).
type PgxTxDBTX struct {
	Tx pgx.Tx
}

func (a *PgxTxDBTX) Query(ctx context.Context, sql string, args ...any) (db.Rows, error) {
	rows, err := a.Tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return &pgxRowsAdapter{rows: rows}, nil
}

func (a *PgxTxDBTX) Exec(ctx context.Context, sql string, args ...any) (db.ExecResult, error) {
	tag, err := a.Tx.Exec(ctx, sql, args...)
	if err != nil {
		return db.ExecResult{}, err
	}
	return db.ExecResult{RowsAffected: tag.RowsAffected()}, nil
}

func (a *PgxTxDBTX) QueryRow(ctx context.Context, sql string, args ...any) db.Row {
	return &pgxRowAdapter{row: a.Tx.QueryRow(ctx, sql, args...)}
}

// pgxRowsAdapter adapts pgx.Rows to db.Rows.
type pgxRowsAdapter struct {
	rows pgx.Rows
}

func (r *pgxRowsAdapter) Next() bool             { return r.rows.Next() }
func (r *pgxRowsAdapter) Scan(dest ...any) error { return r.rows.Scan(dest...) }
func (r *pgxRowsAdapter) Err() error             { return r.rows.Err() }
func (r *pgxRowsAdapter) Close()                 { r.rows.Close() }

// pgxRowAdapter adapts pgx.Row to db.Row.
type pgxRowAdapter struct {
	row pgx.Row
}

func (r *pgxRowAdapter) Scan(dest ...any) error {
	err := r.row.Scan(dest...)
	if err != nil {
		if err == pgx.ErrNoRows {
			return db.ErrNoRows
		}
		return err
	}
	return nil
}

// PoolEnvironmentLocker implements services.EnvironmentLocker using a real pgxpool.Pool.
// It acquires a transaction-scoped advisory lock (pg_advisory_xact_lock) and
// adapts the pgx.Tx to db.DBTX before calling the provided function.
//
// WHY this adapter exists: lock.WithEnvironmentLock returns pgx.Tx, but
// services.EnvironmentLocker expects db.DBTX. This adapter bridges that gap.
type PoolEnvironmentLocker struct {
	Pool *pgxpool.Pool
}

func (l *PoolEnvironmentLocker) WithLock(ctx context.Context, tenant, project, env string, fn func(tx db.DBTX) error) error {
	_, err := lock.WithEnvironmentLock(ctx, l.Pool, tenant, project, env, func(tx lock.Tx) (struct{}, error) {
		adapter := &PgxTxDBTX{Tx: tx}
		return struct{}{}, fn(adapter)
	})
	return err
}
