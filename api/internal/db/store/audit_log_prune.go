// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresAuditLogPruneStore deletes expired audit_log rows for the retention
// pruner (ADR-0047).
type PostgresAuditLogPruneStore struct {
	Pool *pgxpool.Pool
}

// DeleteAuditLogBatch deletes up to limit rows created before cutoff, oldest
// first, and returns how many it deleted.
//
// It runs in its own transaction with `SET LOCAL loopzero.audit_pruner = 'true'`,
// the override the immutability trigger (migration 000014) recognizes. SET
// LOCAL is transaction-scoped, so the bypass ends at COMMIT or ROLLBACK and
// no other session — psql, app code, a runaway query — ever carries it.
func (s *PostgresAuditLogPruneStore) DeleteAuditLogBatch(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SET LOCAL loopzero.audit_pruner = 'true'"); err != nil {
		return 0, fmt.Errorf("set local: %w", err)
	}
	tag, err := tx.Exec(ctx,
		`DELETE FROM audit_log
		 WHERE id IN (
		   SELECT id FROM audit_log
		   WHERE created_at < $1
		   ORDER BY created_at ASC
		   LIMIT $2
		 )`,
		cutoff, limit,
	)
	if err != nil {
		return 0, fmt.Errorf("delete: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return tag.RowsAffected(), nil
}
