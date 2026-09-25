// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/lock"
)

// PoolEnvironmentLocker implements shared.EnvironmentLocker using a real pgxpool.Pool.
// It acquires a transaction-scoped advisory lock (pg_advisory_xact_lock) and
// adapts the pgx.Tx to db.DBTX before calling the provided function.
//
// WHY this adapter exists: lock.WithEnvironmentLock returns pgx.Tx, but
// shared.EnvironmentLocker expects db.DBTX. The tx is wrapped in
// db.PgxTxAdapter — the ONE pgx.Tx bridge — so a statement run under the
// environment lock translates errors (unique violation -> *UniqueViolationError)
// and dispatches to sqlc exactly as one run under the placement lock does.
type PoolEnvironmentLocker struct {
	Pool *pgxpool.Pool
}

func (l *PoolEnvironmentLocker) WithLock(ctx context.Context, tenant, project, env string, fn func(tx db.DBTX) error) error {
	_, err := lock.WithEnvironmentLock(ctx, l.Pool, tenant, project, env, func(tx lock.Tx) (struct{}, error) {
		return struct{}{}, fn(&db.PgxTxAdapter{Tx: tx})
	})
	return err
}

// The port this adapter satisfies, asserted at compile time.
var _ deploycontracts.EnvironmentLocker = (*PoolEnvironmentLocker)(nil)
