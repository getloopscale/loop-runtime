// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/generated"
	"github.com/getloopscale/loopzero/api/internal/lock"
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

// GeneratedDB returns the underlying pgx.Tx as a generated.DBTX (sqlc-
// emitted pgx-native interface). Bridges PgxTxDBTX into sqlc-backed
// stores so that calls executed inside withEnvironmentLock can
// dispatch through SqlcBridge with the lock's transaction (not the
// pool) — preserving the advisory-lock isolation invariant.
//
// Fixes the LIVE panic surfaced in the 2026-05-29 cleanroom dogfood
// run:
//   "build_wrapper: DBTX *adapters.PgxTxDBTX does not implement
//    SqlcBridge — sqlc-backed stores require PgxAdapter /
//    PgxTxAdapter or pgxmock per T0.6 reshape matrix"
//
// Root cause: PgxTxDBTX is the lock-side tx adapter (this package);
// PgxTxAdapter is the placement-side tx adapter (api/internal/db).
// Both wrap pgx.Tx for the same db.DBTX interface but only the
// latter implemented SqlcBridge before this fix. The wrapper
// dispatch via `tx.(SqlcBridge)` was failing the assertion in the
// lock path. Adding GeneratedDB() here makes the lock-side adapter
// satisfy the same bridge contract as the placement-side adapter
// — uniform sqlc dispatch regardless of which tx-wrapping path
// the caller took.
func (a *PgxTxDBTX) GeneratedDB() generated.DBTX {
	return a.Tx
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
		if errors.Is(err, pgx.ErrNoRows) {
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

// Compile-time assertion that PgxTxDBTX satisfies BOTH db.DBTX AND
// db.SqlcBridge. The latter is critical: the lock-side adapter is
// dispatched into sqlc-backed wrappers (via bridge() type-assertion)
// during withEnvironmentLock-held mutations. Pre-fix-949aae1c, this
// assertion was missing, the type satisfied DBTX but not SqlcBridge,
// and the LIVE cleanroom dogfood retry surfaced the gap as a panic
// in 18 of 52 specs (build_wrapper: bridge() type-assertion failed).
//
// If a future migration adds a NEW pgx.Tx wrapping adapter, the
// equivalent compile-time assertions MUST be added — otherwise the
// same class of bug recurs.
var _ db.DBTX = (*PgxTxDBTX)(nil)
var _ db.SqlcBridge = (*PgxTxDBTX)(nil)
