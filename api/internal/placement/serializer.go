// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewAdvisoryLockSerializer returns a Serializer that acquires a
// transaction-scoped Postgres advisory lock before invoking the callback.
// Use this in production so concurrent `AssignCell` calls — even across
// multiple API replicas — serialize at the database level.
//
// Contract:
//  1. Open a transaction on the given pool.
//  2. `SELECT pg_advisory_xact_lock(PlacementLockID)` — blocks until the
//     lock is free. Transaction-scoped so the lock releases on commit,
//     rollback, or connection death.
//  3. Run the callback inside the same transaction context.
//  4. On callback success, commit. On callback error, rollback.
//
// Roadmap 06 DoD: this is the wrapper the live API startup uses to make
// `AssignCell` atomic across replicas. It complements Rule 7
// (`pg_advisory_xact_lock` only — never session-scoped).
//
// When nil is returned the caller should fall back to calling AssignCell
// without a serializer (single-replica is-safe-by-default). NewAdvisoryLock
// Serializer only returns nil when the given pool is nil, which lets the
// API bootstrap code unconditionally wire the serializer and degrade
// gracefully during local dev without a database.
func NewAdvisoryLockSerializer(pool *pgxpool.Pool) Serializer {
	if pool == nil {
		return nil
	}
	return func(ctx context.Context, fn func(ctx context.Context) error) error {
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return fmt.Errorf("placement serializer: begin tx: %w", err)
		}
		// Ensure the transaction closes even if the callback panics.
		var committed bool
		defer func() {
			if !committed {
				_ = tx.Rollback(ctx)
			}
		}()

		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", PlacementLockID); err != nil {
			return fmt.Errorf("placement serializer: acquire advisory lock: %w", err)
		}

		if err := fn(ctx); err != nil {
			return err // caller wants this error verbatim; defer rolls back
		}

		if err := tx.Commit(ctx); err != nil {
			if errors.Is(err, pgx.ErrTxClosed) {
				// Nothing to commit — benign race with another cleanup.
				committed = true
				return nil
			}
			return fmt.Errorf("placement serializer: commit: %w", err)
		}
		committed = true
		return nil
	}
}
