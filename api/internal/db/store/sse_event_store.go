// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/ports"
)

// pruneBatchSize limits how many rows a single DELETE removes to avoid
// long-running locks and I/O spikes when pruning large backlogs.
const pruneBatchSize = 5000

// PostgresEventStore implements ports.EventStore (the SSE event journal)
// over the sse_events table. The HTTP streams in package sse read it through
// the port; this package owns the SQL.
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

// Append inserts a new event into the sse_events table and returns the
// generated sequential ID. Streams discover it by polling (see pollCadence);
// nothing LISTENs on a notification channel, so Append sends no NOTIFY.
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

	return id, nil
}

// GetAfter returns all events for a build with ID greater than afterID, ordered by ID ascending.
// Returns an empty slice (not nil) when no events match.
func (s *PostgresEventStore) GetAfter(ctx context.Context, buildID string, afterID int64) ([]ports.SSEEvent, error) {
	rows, err := s.queryDB.Query(ctx,
		"SELECT id, build_id, event_type, data, created_at FROM sse_events WHERE build_id = $1 AND id > $2 ORDER BY id ASC LIMIT 1000",
		buildID, afterID,
	)
	if err != nil {
		return nil, fmt.Errorf("event store: get after failed: %w", err)
	}
	defer rows.Close()

	events := make([]ports.SSEEvent, 0)
	for rows.Next() {
		var ev ports.SSEEvent
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
func (s *PostgresEventStore) GetAfterForProject(ctx context.Context, tenantID string, projectName string, afterID int64) ([]ports.SSEEvent, error) {
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

	events := make([]ports.SSEEvent, 0)
	for rows.Next() {
		var ev ports.SSEEvent
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

// GetSignalsAfter returns the signal.changed events of one environment of a
// project, after afterID. Those events are appended by
// ReleaseStoreSqlc.AppendSideEffect keyed by the RELEASE id (sse_events has a
// build_id column but no foreign key), so they are joined through releases —
// joining builds, as GetAfterForProject does, matches none of them.
//
// SECURITY: tenantID is enforced inside the SQL, as in GetAfterForProject.
// releases.project_id stores the project NAME (CLAUDE.md convention).
func (s *PostgresEventStore) GetSignalsAfter(ctx context.Context, tenantID, projectName, environment string, afterID int64) ([]ports.SSEEvent, error) {
	rows, err := s.queryDB.Query(ctx, `
		SELECT se.id, se.build_id, se.event_type, se.data, se.created_at
		FROM sse_events se
		JOIN releases r ON r.id = se.build_id
		WHERE r.tenant_id = $1 AND r.project_id = $2 AND r.environment = $3
		  AND se.event_type = 'signal.changed' AND se.id > $4
		ORDER BY se.id ASC
		LIMIT 1000
	`, tenantID, projectName, environment, afterID)
	if err != nil {
		return nil, fmt.Errorf("event store: get signals after failed: %w", err)
	}
	defer rows.Close()

	events := make([]ports.SSEEvent, 0)
	for rows.Next() {
		var ev ports.SSEEvent
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
