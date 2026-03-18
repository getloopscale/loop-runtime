package sse

import (
	"context"
	"encoding/json"
	"fmt"
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
	Prune(ctx context.Context, olderThan time.Duration) (int64, error)
}

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

// Append inserts a new event into the sse_events table and returns the generated sequential ID.
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

// Prune deletes events older than the given duration and returns the count of deleted rows.
func (s *PostgresEventStore) Prune(ctx context.Context, olderThan time.Duration) (int64, error) {
	threshold := time.Now().Add(-olderThan)

	result, err := s.execDB.Exec(ctx,
		"DELETE FROM sse_events WHERE created_at < $1",
		threshold,
	)
	if err != nil {
		return 0, fmt.Errorf("event store: prune failed: %w", err)
	}

	return result.RowsAffected, nil
}
