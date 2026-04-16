// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package workqueue provides a PostgreSQL-backed distributed work queue.
//
// Work items are enqueued by producers (typically the leader pod's reconcile loops)
// and dequeued by consumers (any pod, leader or non-leader). This eliminates the
// singleton bottleneck where all background processing runs on a single leader.
//
// Uses SELECT ... FOR UPDATE SKIP LOCKED for lock-free concurrent processing.
// Multiple pods can safely dequeue without coordination.
//
// P2-8: Decouple leader-only loops for horizontal scalability.
package workqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// QueueName identifies a work queue. Each background loop gets its own queue.
type QueueName string

const (
	QueueBuildReconcile    QueueName = "build_reconcile"
	QueueReleaseConverge   QueueName = "release_converge"
	QueueEventPrune        QueueName = "event_prune"
	QueueIdempotencyClean  QueueName = "idempotency_clean"
	QueueBuildReap         QueueName = "build_reap"
	QueueReleaseReap       QueueName = "release_reap"
)

// WorkItem represents a dequeued item ready for processing.
type WorkItem struct {
	ID       int64
	Queue    QueueName
	Payload  json.RawMessage
	Attempts int
}

// Queue provides enqueue/dequeue operations backed by PostgreSQL.
type Queue struct {
	pool     *pgxpool.Pool
	logger   *slog.Logger
	hostname string
}

// New creates a work queue backed by the given connection pool.
func New(pool *pgxpool.Pool, logger *slog.Logger) *Queue {
	hostname, _ := os.Hostname()
	return &Queue{
		pool:     pool,
		logger:   logger,
		hostname: hostname,
	}
}

// Enqueue adds a work item to the specified queue.
// Idempotent: duplicate payloads for the same queue are safe (processed independently).
func (q *Queue) Enqueue(ctx context.Context, queue QueueName, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("workqueue: marshal payload: %w", err)
	}

	_, err = q.pool.Exec(ctx,
		`INSERT INTO work_queue (queue, payload) VALUES ($1, $2)`,
		string(queue), data)
	if err != nil {
		return fmt.Errorf("workqueue: enqueue %s: %w", queue, err)
	}
	return nil
}

// Dequeue claims up to `limit` pending items from the queue.
// Uses FOR UPDATE SKIP LOCKED — concurrent dequeue is safe without coordination.
// Returns empty slice if no work available (not an error).
func (q *Queue) Dequeue(ctx context.Context, queue QueueName, limit int) ([]WorkItem, error) {
	rows, err := q.pool.Query(ctx, `
		UPDATE work_queue
		SET status = 'processing', claimed_by = $1, claimed_at = NOW(), attempts = attempts + 1, updated_at = NOW()
		WHERE id IN (
			SELECT id FROM work_queue
			WHERE queue = $2 AND status = 'pending' AND attempts < max_attempts
			ORDER BY created_at ASC
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, queue, payload, attempts
	`, q.hostname, string(queue), limit)
	if err != nil {
		return nil, fmt.Errorf("workqueue: dequeue %s: %w", queue, err)
	}
	defer rows.Close()

	var items []WorkItem
	for rows.Next() {
		var item WorkItem
		var queueStr string
		if err := rows.Scan(&item.ID, &queueStr, &item.Payload, &item.Attempts); err != nil {
			return nil, fmt.Errorf("workqueue: scan: %w", err)
		}
		item.Queue = QueueName(queueStr)
		items = append(items, item)
	}

	return items, rows.Err()
}

// Complete marks a work item as successfully processed.
func (q *Queue) Complete(ctx context.Context, id int64) error {
	_, err := q.pool.Exec(ctx,
		`UPDATE work_queue SET status = 'completed', completed_at = NOW(), updated_at = NOW() WHERE id = $1`,
		id)
	return err
}

// Fail marks a work item as failed with an error message.
// If max_attempts is not reached, the item stays available for retry.
func (q *Queue) Fail(ctx context.Context, id int64, errMsg string) error {
	_, err := q.pool.Exec(ctx, `
		UPDATE work_queue
		SET status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
		    error = $2, updated_at = NOW(), claimed_by = NULL, claimed_at = NULL
		WHERE id = $1
	`, id, errMsg)
	return err
}

// ReclaimStale returns items stuck in 'processing' for longer than timeout
// back to 'pending' status. Called periodically by the leader to handle pod crashes.
func (q *Queue) ReclaimStale(ctx context.Context, timeout time.Duration) (int64, error) {
	tag, err := q.pool.Exec(ctx, `
		UPDATE work_queue
		SET status = 'pending', claimed_by = NULL, claimed_at = NULL, updated_at = NOW()
		WHERE status = 'processing' AND claimed_at < NOW() - $1::interval
	`, timeout.String())
	if err != nil {
		return 0, fmt.Errorf("workqueue: reclaim stale: %w", err)
	}
	return tag.RowsAffected(), nil
}

// PruneCompleted deletes completed items older than the given age.
func (q *Queue) PruneCompleted(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := q.pool.Exec(ctx, `
		DELETE FROM work_queue
		WHERE status IN ('completed', 'failed') AND completed_at < NOW() - $1::interval
	`, olderThan.String())
	if err != nil {
		return 0, fmt.Errorf("workqueue: prune: %w", err)
	}
	return tag.RowsAffected(), nil
}

// QueueDepth returns the count of pending items for a queue.
func (q *Queue) QueueDepth(ctx context.Context, queue QueueName) (int64, error) {
	var count int64
	err := q.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM work_queue WHERE queue = $1 AND status = 'pending'`,
		string(queue)).Scan(&count)
	return count, err
}
