// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package placement

import (
	"context"
	"errors"
	"fmt"
	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/dbtx"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrPlacementLockTimeout is returned when the advisory lock cannot be
// acquired within the session-local `lock_timeout` (T1.6 of
// deep-review-2026-05-10-remediation-plan). Distinguishes "the system is
// busy with another tenant placement" from "the system is broken" — the
// caller can retry or surface 503 instead of 500.
var ErrPlacementLockTimeout = errors.New("placement serializer: lock_timeout exceeded")

// isLockTimeoutErr reports whether err is the PG SQLSTATE 55P03
// (lock_not_available) — the canonical signal that lock_timeout fired.
func isLockTimeoutErr(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "55P03"
	}
	return false
}

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

		// T1.6 of deep-review-2026-05-10-remediation-plan (find_conc_placement_no_timeout HIGH):
		// set a session-local lock_timeout so a slow caller cannot block
		// the placement worker indefinitely. SQLSTATE 55P03 maps to a
		// typed error so callers can distinguish "system busy" from
		// "system broken".
		if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '30s'"); err != nil {
			return fmt.Errorf("placement serializer: set lock_timeout: %w", err)
		}

		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", PlacementLockID); err != nil {
			if isLockTimeoutErr(err) {
				return ErrPlacementLockTimeout
			}
			return fmt.Errorf("placement serializer: acquire advisory lock: %w", err)
		}

		// MEET-053 D-01: carry tx in the context (dbtx.ContextWithTx) so the
		// tenant INSERT joins the transaction that holds the lock. This fixes
		// the TOCTOU bug where AssignCell ran under the lock but the INSERT ran
		// on a separate connection.
		txCtx := dbtx.ContextWithTx(ctx, &db.PgxTxAdapter{Tx: tx})

		if err := fn(txCtx); err != nil {
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
