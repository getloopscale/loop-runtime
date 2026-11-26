// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package sse_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/sse"
)

// --- Compile-time interface check ---

func TestPostgresEventStoreImplementsInterface(t *testing.T) {
	var _ sse.EventStore = (*store.PostgresEventStore)(nil)
}

// --- Fakes ---

// fakeSSERow represents a single row stored by the fake.
type fakeSSERow struct {
	id        int64
	buildID   string
	eventType string
	data      json.RawMessage
	createdAt time.Time
}

// fakeSSEQueryExec implements both db.QueryExecutor and db.ExecQueryExecutor
// for unit testing the event store without a real database.
type fakeSSEQueryExec struct {
	rows      []fakeSSERow
	nextID    int64
	execCalls []fakeExecCall
}

type fakeExecCall struct {
	sql  string
	args []any
}

func newFakeSSEQueryExec() *fakeSSEQueryExec {
	return &fakeSSEQueryExec{
		nextID: 1,
	}
}

func (f *fakeSSEQueryExec) Query(ctx context.Context, sql string, args ...any) (db.Rows, error) {
	// Detect INSERT ... RETURNING (Append)
	if len(args) >= 3 && isInsertReturning(sql) {
		buildID := args[0].(string)
		eventType := args[1].(string)
		data := args[2].(json.RawMessage)

		id := f.nextID
		f.nextID++

		row := fakeSSERow{
			id:        id,
			buildID:   buildID,
			eventType: eventType,
			data:      data,
			createdAt: time.Now(),
		}
		f.rows = append(f.rows, row)

		return &fakeSSERows{
			rows:    []fakeSSERow{row},
			scanOne: true, // only scan id
		}, nil
	}

	// Detect SELECT (GetAfter)
	buildID := args[0].(string)
	afterID := args[1].(int64)

	var result []fakeSSERow
	for _, r := range f.rows {
		if r.buildID == buildID && r.id > afterID {
			result = append(result, r)
		}
	}

	return &fakeSSERows{rows: result}, nil
}

func (f *fakeSSEQueryExec) Exec(ctx context.Context, sql string, args ...any) (db.ExecResult, error) {
	f.execCalls = append(f.execCalls, fakeExecCall{sql: sql, args: args})

	// A-02: pg_notify calls are no-ops in tests
	if strings.Contains(sql, "pg_notify") {
		return db.ExecResult{}, nil
	}

	// Detect Prune: DELETE WHERE created_at < threshold
	if len(args) >= 1 {
		threshold := args[0].(time.Time)
		var remaining []fakeSSERow
		var deleted int64
		for _, r := range f.rows {
			if r.createdAt.Before(threshold) {
				deleted++
			} else {
				remaining = append(remaining, r)
			}
		}
		f.rows = remaining
		return db.ExecResult{RowsAffected: deleted}, nil
	}

	return db.ExecResult{}, nil
}

func isInsertReturning(sql string) bool {
	return len(sql) > 6 && sql[:6] == "INSERT"
}

// fakeSSERows implements db.Rows for the fake.
type fakeSSERows struct {
	rows    []fakeSSERow
	cursor  int
	scanOne bool // if true, Scan only provides id (for INSERT RETURNING)
}

func (r *fakeSSERows) Next() bool {
	return r.cursor < len(r.rows)
}

func (r *fakeSSERows) Scan(dest ...any) error {
	row := r.rows[r.cursor]
	r.cursor++

	if r.scanOne {
		// INSERT ... RETURNING id
		*(dest[0].(*int64)) = row.id
		return nil
	}

	// SELECT id, build_id, event_type, data, created_at
	*(dest[0].(*int64)) = row.id
	*(dest[1].(*string)) = row.buildID
	*(dest[2].(*string)) = row.eventType
	*(dest[3].(*json.RawMessage)) = row.data
	*(dest[4].(*time.Time)) = row.createdAt
	return nil
}

func (r *fakeSSERows) Err() error { return nil }
func (r *fakeSSERows) Close()     {}

// --- Tests ---

func TestAppendReturnsSequentialID(t *testing.T) {
	fake := newFakeSSEQueryExec()
	eventStore := store.NewPostgresEventStore(fake, fake)

	ctx := context.Background()
	data := json.RawMessage(`{"step":"clone"}`)

	id1, err := eventStore.Append(ctx, "build-1", "progress", data)
	if err != nil {
		t.Fatalf("Append 1 failed: %v", err)
	}

	id2, err := eventStore.Append(ctx, "build-1", "progress", data)
	if err != nil {
		t.Fatalf("Append 2 failed: %v", err)
	}

	id3, err := eventStore.Append(ctx, "build-1", "progress", data)
	if err != nil {
		t.Fatalf("Append 3 failed: %v", err)
	}

	if id1 >= id2 || id2 >= id3 {
		t.Errorf("expected monotonically increasing IDs, got %d, %d, %d", id1, id2, id3)
	}
}

func TestGetAfterReturnsEventsAfterID(t *testing.T) {
	fake := newFakeSSEQueryExec()
	eventStore := store.NewPostgresEventStore(fake, fake)

	ctx := context.Background()

	// Append 5 events
	var ids []int64
	for i := 0; i < 5; i++ {
		data := json.RawMessage(`{"step":"` + string(rune('a'+i)) + `"}`)
		id, err := eventStore.Append(ctx, "build-1", "progress", data)
		if err != nil {
			t.Fatalf("Append %d failed: %v", i, err)
		}
		ids = append(ids, id)
	}

	// GetAfter id=2 should return events 3, 4, 5
	events, err := eventStore.GetAfter(ctx, "build-1", ids[1])
	if err != nil {
		t.Fatalf("GetAfter failed: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	for i, ev := range events {
		expectedID := ids[2+i]
		if ev.ID != expectedID {
			t.Errorf("event %d: expected ID %d, got %d", i, expectedID, ev.ID)
		}
	}
}

func TestGetAfterWithNonexistentIDReturnsEmpty(t *testing.T) {
	fake := newFakeSSEQueryExec()
	eventStore := store.NewPostgresEventStore(fake, fake)

	ctx := context.Background()

	// Append one event
	_, err := eventStore.Append(ctx, "build-1", "progress", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// GetAfter with very high ID
	events, err := eventStore.GetAfter(ctx, "build-1", 999999)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if events == nil {
		t.Fatal("expected empty slice, got nil")
	}

	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestGetAfterOrdering(t *testing.T) {
	fake := newFakeSSEQueryExec()
	eventStore := store.NewPostgresEventStore(fake, fake)

	ctx := context.Background()

	// Append events with different types to verify ordering
	types := []string{"clone", "build", "push", "deploy"}
	for _, et := range types {
		_, err := eventStore.Append(ctx, "build-1", et, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("Append %s failed: %v", et, err)
		}
	}

	events, err := eventStore.GetAfter(ctx, "build-1", 0)
	if err != nil {
		t.Fatalf("GetAfter failed: %v", err)
	}

	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}

	// Verify insertion order is preserved
	for i, ev := range events {
		if ev.EventType != types[i] {
			t.Errorf("event %d: expected type %q, got %q", i, types[i], ev.EventType)
		}
	}

	// Verify IDs are strictly increasing
	for i := 1; i < len(events); i++ {
		if events[i].ID <= events[i-1].ID {
			t.Errorf("events not in order: event %d (ID=%d) <= event %d (ID=%d)",
				i, events[i].ID, i-1, events[i-1].ID)
		}
	}
}

func TestPruneRemovesOldEvents(t *testing.T) {
	fake := newFakeSSEQueryExec()
	eventStore := store.NewPostgresEventStore(fake, fake)

	ctx := context.Background()

	// Insert events (they get time.Now() as createdAt in the fake)
	for i := 0; i < 3; i++ {
		_, err := eventStore.Append(ctx, "build-1", "progress", json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("Append %d failed: %v", i, err)
		}
	}

	// Manually set old timestamps on first 2 events in the fake
	oldTime := time.Now().Add(-2 * time.Hour)
	fake.rows[0].createdAt = oldTime
	fake.rows[1].createdAt = oldTime

	// Prune events older than 1 hour
	count, err := eventStore.Prune(ctx, 1*time.Hour)
	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}

	if count != 2 {
		t.Errorf("expected 2 pruned, got %d", count)
	}

	// Verify remaining events
	events, err := eventStore.GetAfter(ctx, "build-1", 0)
	if err != nil {
		t.Fatalf("GetAfter after prune failed: %v", err)
	}

	if len(events) != 1 {
		t.Errorf("expected 1 remaining event, got %d", len(events))
	}
}

func TestPruneReturnsCount(t *testing.T) {
	fake := newFakeSSEQueryExec()
	eventStore := store.NewPostgresEventStore(fake, fake)

	ctx := context.Background()

	// Insert 5 events
	for i := 0; i < 5; i++ {
		_, err := eventStore.Append(ctx, "build-1", "progress", json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("Append %d failed: %v", i, err)
		}
	}

	// Mark 3 events as old
	oldTime := time.Now().Add(-48 * time.Hour)
	for i := 0; i < 3; i++ {
		fake.rows[i].createdAt = oldTime
	}

	count, err := eventStore.Prune(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}

	if count != 3 {
		t.Errorf("expected prune count 3, got %d", count)
	}

	// Prune again with nothing old — should return 0
	count, err = eventStore.Prune(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("Prune (second call) failed: %v", err)
	}

	if count != 0 {
		t.Errorf("expected prune count 0, got %d", count)
	}
}
