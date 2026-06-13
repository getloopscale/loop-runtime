// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
)

// SSEEvent represents a single event in the SSE event journal.
type SSEEvent struct {
	ID        int64           `json:"id"`
	BuildID   string          `json:"build_id"`
	EventType string          `json:"event_type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

// EventStore defines the contract for SSE event persistence.
// Supports append, replay (via Last-Event-ID), and cleanup.
type EventStore interface {
	Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	GetAfter(ctx context.Context, buildID string, afterID int64) ([]SSEEvent, error)
	// GetAfterForProject returns sse_events for ALL builds owned by
	// (tenantID, projectName) whose id > afterID. Plan J-17 T2.3 — backs
	// the project-level stream that powers the realtime deployments list.
	// Implementations MUST enforce tenant ownership in the SQL itself
	// (defense-in-depth — handler also checks, but the store closes the
	// vulnerability window between auth and query).
	GetAfterForProject(ctx context.Context, tenantID string, projectName string, afterID int64) ([]SSEEvent, error)
	Prune(ctx context.Context, olderThan time.Duration) (int64, error)
}

// pruneBatchSize limits how many rows a single DELETE removes to avoid
// long-running locks and I/O spikes when pruning large backlogs.
const pruneBatchSize = 5000

// PostgresEventStore implements EventStore using PostgreSQL.
type PostgresEventStore struct {
	queryDB db.QueryExecutor
	execDB  db.ExecQueryExecutor
}

// NewPostgresEventStore creates a new PostgresEventStore.
// queryDB is used for SELECT and INSERT...RETURNING queries.
// execDB is used for DELETE (prune) operations.
func NewPostgresEventStore(queryDB db.QueryExecutor, execDB db.ExecQueryExecutor) *PostgresEventStore {
	return &PostgresEventStore{
		queryDB: queryDB,
		execDB:  execDB,
	}
}

// Append inserts a new event into the sse_events table, sends a NOTIFY
// on the "sse_events" channel, and returns the generated sequential ID.
//
// A-02 fix: The NOTIFY allows SSE stream handlers to wake up immediately
// when new events are available instead of polling every second. This
// reduces PostgreSQL QPS from ~200 (one per stream per second) to ~N
// (one per actual event emitted, typically <10/s).
func (s *PostgresEventStore) Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error) {
	rows, err := s.queryDB.Query(ctx,
		"INSERT INTO sse_events (build_id, event_type, data) VALUES ($1, $2, $3) RETURNING id",
		buildID, eventType, data,
	)
	if err != nil {
		return 0, fmt.Errorf("event store: append failed: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return 0, fmt.Errorf("event store: append returned no ID")
	}

	var id int64
	if err := rows.Scan(&id); err != nil {
		return 0, fmt.Errorf("event store: scan ID failed: %w", err)
	}

	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("event store: rows error: %w", err)
	}

	// Send NOTIFY to wake up SSE listeners for this build.
	// Best-effort: if NOTIFY fails, SSE handlers will fall back to polling.
	//
	// Plan J-17 EC-2 (2026-06-06): PostgreSQL pg_notify silently
	// truncates payloads larger than 8000 bytes. The payload here is
	// just `build_id` (~16 chars), so we are nowhere near the limit.
	// NEVER expand this payload to include the event body — listeners
	// already SELECT the event row after waking, so the body would
	// duplicate the work AND risk silent truncation that loses events.
	if _, err := s.execDB.Exec(ctx,
		"SELECT pg_notify('sse_events', $1)",
		buildID,
	); err != nil {
		slog.Warn("event store: pg_notify failed, SSE listeners will fall back to polling",
			"build_id", buildID, "error", err)
	}

	return id, nil
}

// GetAfter returns all events for a build with ID greater than afterID, ordered by ID ascending.
// Returns an empty slice (not nil) when no events match.
func (s *PostgresEventStore) GetAfter(ctx context.Context, buildID string, afterID int64) ([]SSEEvent, error) {
	rows, err := s.queryDB.Query(ctx,
		"SELECT id, build_id, event_type, data, created_at FROM sse_events WHERE build_id = $1 AND id > $2 ORDER BY id ASC LIMIT 1000",
		buildID, afterID,
	)
	if err != nil {
		return nil, fmt.Errorf("event store: get after failed: %w", err)
	}
	defer rows.Close()

	events := make([]SSEEvent, 0)
	for rows.Next() {
		var ev SSEEvent
		if err := rows.Scan(&ev.ID, &ev.BuildID, &ev.EventType, &ev.Data, &ev.CreatedAt); err != nil {
			return nil, fmt.Errorf("event store: scan failed: %w", err)
		}
		events = append(events, ev)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("event store: rows error: %w", err)
	}

	return events, nil
}

// GetAfterForProject returns events for every build owned by
// (tenantID, projectName) whose id is greater than afterID, ordered by id ASC.
// Plan J-17 T2.3 — backs /api/v1/projects/{name}/builds/stream so the
// dashboard list page sees new builds + status transitions without polling.
//
// SECURITY: tenantID is enforced inside the SQL — this is the canonical
// authorization for project-level streaming and MUST NOT be relaxed.
// builds.project_id stores the project NAME (CLAUDE.md convention).
func (s *PostgresEventStore) GetAfterForProject(ctx context.Context, tenantID string, projectName string, afterID int64) ([]SSEEvent, error) {
	rows, err := s.queryDB.Query(ctx, `
		SELECT se.id, se.build_id, se.event_type, se.data, se.created_at
		FROM sse_events se
		JOIN builds b ON b.id = se.build_id
		WHERE b.tenant_id = $1 AND b.project_id = $2 AND se.id > $3
		ORDER BY se.id ASC
		LIMIT 1000
	`, tenantID, projectName, afterID)
	if err != nil {
		return nil, fmt.Errorf("event store: get after for project failed: %w", err)
	}
	defer rows.Close()

	events := make([]SSEEvent, 0)
	for rows.Next() {
		var ev SSEEvent
		if err := rows.Scan(&ev.ID, &ev.BuildID, &ev.EventType, &ev.Data, &ev.CreatedAt); err != nil {
			return nil, fmt.Errorf("event store: scan failed: %w", err)
		}
		events = append(events, ev)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("event store: rows error: %w", err)
	}

	return events, nil
}

// Prune deletes events older than the given duration and returns the total count of deleted rows.
// MOD-2: Deletes in batches of pruneBatchSize to prevent long-running locks and I/O spikes
// when pruning large backlogs (e.g., after pruner was down for days).
func (s *PostgresEventStore) Prune(ctx context.Context, olderThan time.Duration) (int64, error) {
	threshold := time.Now().Add(-olderThan)
	var totalDeleted int64

	for {
		if ctx.Err() != nil {
			return totalDeleted, ctx.Err()
		}

		result, err := s.execDB.Exec(ctx,
			"DELETE FROM sse_events WHERE id IN (SELECT id FROM sse_events WHERE created_at < $1 LIMIT $2)",
			threshold, pruneBatchSize,
		)
		if err != nil {
			return totalDeleted, fmt.Errorf("event store: prune failed: %w", err)
		}

		totalDeleted += result.RowsAffected
		if result.RowsAffected < int64(pruneBatchSize) {
			break
		}
	}

	return totalDeleted, nil
}
