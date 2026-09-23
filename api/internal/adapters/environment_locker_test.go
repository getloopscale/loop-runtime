// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loopzero/api/internal/db"
)

// A unique-constraint violation must reach the caller as *db.UniqueViolationError
// whichever path opened the transaction: stores map it to ErrProjectExists and
// similar, and under withEnvironmentLock it used to arrive as a raw pgconn error
// because the lock path wrapped the tx in a second adapter that did not
// translate it.
func TestPoolEnvironmentLocker_UniqueViolationIsTyped(t *testing.T) {
	dsn := os.Getenv("LOOPZERO_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("LOOPZERO_E2E_DATABASE_URL not set — real-PG environment lock test skipped")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	locker := &PoolEnvironmentLocker{Pool: pool}
	err = locker.WithLock(ctx, "tn-lock-uv", "web", "production", func(tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `CREATE TEMP TABLE lock_uv (k int PRIMARY KEY) ON COMMIT DROP`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO lock_uv VALUES (1)`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO lock_uv VALUES (1)`)
		return err
	})
	var uv *db.UniqueViolationError
	if !errors.As(err, &uv) {
		t.Fatalf("want *db.UniqueViolationError, got %T: %v", err, err)
	}
}
