// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// =============================================================================
// PostgresEventStore tests — cover Append, GetAfter, Prune
// =============================================================================

// fakeQueryExecutor implements db.QueryExecutor for testing.
type fakeQueryExecutor struct {
	queryRows db.Rows
	queryErr  error
}

func (f *fakeQueryExecutor) Query(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	return f.queryRows, f.queryErr
}

func (f *fakeQueryExecutor) QueryRow(_ context.Context, _ string, _ ...any) db.Row {
	return nil
}

// fakeExecQueryExecutor implements db.ExecQueryExecutor for testing.
type fakeExecQueryExecutor struct {
	execResult db.ExecResult
	execErr    error
	queryRows  db.Rows
	queryErr   error
}

func (f *fakeExecQueryExecutor) Exec(_ context.Context, _ string, _ ...any) (db.ExecResult, error) {
	return f.execResult, f.execErr
}

func (f *fakeExecQueryExecutor) Query(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	return f.queryRows, f.queryErr
}

func (f *fakeExecQueryExecutor) QueryRow(_ context.Context, _ string, _ ...any) db.Row {
	return nil
}

// fakeRows implements db.Rows for testing.
type fakeRows struct {
	items   [][]any
	current int
	scanErr error
	errVal  error
}

func (r *fakeRows) Next() bool {
	if r.current < len(r.items) {
		r.current++
		return true
	}
	return false
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	row := r.items[r.current-1]
	for i := range dest {
		if i < len(row) {
			switch d := dest[i].(type) {
			case *int64:
				if v, ok := row[i].(int64); ok {
					*d = v
				}
			case *string:
				if v, ok := row[i].(string); ok {
					*d = v
				}
			case *json.RawMessage:
				if v, ok := row[i].(json.RawMessage); ok {
					*d = v
				}
			case *time.Time:
				if v, ok := row[i].(time.Time); ok {
					*d = v
				}
			}
		}
	}
	return nil
}

func (r *fakeRows) Err() error { return r.errVal }
func (r *fakeRows) Close()     {}

func TestNewPostgresEventStore(t *testing.T) {
	store := NewPostgresEventStore(nil, nil)
	if store == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestPostgresEventStore_Append_QueryError(t *testing.T) {
	qe := &fakeQueryExecutor{queryErr: errors.New("db down")}
	ee := &fakeExecQueryExecutor{}
	store := NewPostgresEventStore(qe, ee)

	_, err := store.Append(context.Background(), "bld-1", "build.started", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPostgresEventStore_Append_NoRows(t *testing.T) {
	rows := &fakeRows{items: nil} // no rows = no ID returned
	qe := &fakeQueryExecutor{queryRows: rows}
	ee := &fakeExecQueryExecutor{}
	store := NewPostgresEventStore(qe, ee)

	_, err := store.Append(context.Background(), "bld-1", "build.started", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for no returned ID")
	}
}

func TestPostgresEventStore_Append_Success(t *testing.T) {
	rows := &fakeRows{
		items: [][]any{{int64(42)}},
	}
	qe := &fakeQueryExecutor{queryRows: rows}
	ee := &fakeExecQueryExecutor{}
	store := NewPostgresEventStore(qe, ee)

	id, err := store.Append(context.Background(), "bld-1", "build.started", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 42 {
		t.Errorf("expected ID 42, got %d", id)
	}
}

// countingExec records every statement Append sends through the exec path.
type countingExec struct {
	fakeExecQueryExecutor
	execs int
}

func (c *countingExec) Exec(ctx context.Context, sql string, args ...any) (db.ExecResult, error) {
	c.execs++
	return c.fakeExecQueryExecutor.Exec(ctx, sql, args...)
}

// Nothing LISTENs on a notification channel — streams poll the table — so
// Append is exactly one INSERT and pays no second round-trip.
func TestPostgresEventStore_Append_IssuesOnlyTheInsert(t *testing.T) {
	rows := &fakeRows{items: [][]any{{int64(7)}}}
	exec := &countingExec{}
	store := NewPostgresEventStore(&fakeQueryExecutor{queryRows: rows}, exec)

	if _, err := store.Append(context.Background(), "bld-1", "build.started", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("append: %v", err)
	}
	if exec.execs != 0 {
		t.Fatalf("Append sent %d extra statement(s) besides the INSERT", exec.execs)
	}
}

func TestPostgresEventStore_GetAfter_QueryError(t *testing.T) {
	qe := &fakeQueryExecutor{queryErr: errors.New("db down")}
	ee := &fakeExecQueryExecutor{}
	store := NewPostgresEventStore(qe, ee)

	_, err := store.GetAfter(context.Background(), "bld-1", 0)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPostgresEventStore_GetAfter_Empty(t *testing.T) {
	rows := &fakeRows{items: nil}
	qe := &fakeQueryExecutor{queryRows: rows}
	ee := &fakeExecQueryExecutor{}
	store := NewPostgresEventStore(qe, ee)

	events, err := store.GetAfter(context.Background(), "bld-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if events == nil {
		t.Error("expected non-nil empty slice")
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestPostgresEventStore_GetAfter_WithEvents(t *testing.T) {
	now := time.Now()
	rows := &fakeRows{
		items: [][]any{
			{int64(1), "bld-1", "build.started", json.RawMessage(`{"status":"running"}`), now},
			{int64(2), "bld-1", "build.completed", json.RawMessage(`{"status":"succeeded"}`), now},
		},
	}
	qe := &fakeQueryExecutor{queryRows: rows}
	ee := &fakeExecQueryExecutor{}
	store := NewPostgresEventStore(qe, ee)

	events, err := store.GetAfter(context.Background(), "bld-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].EventType != "build.started" {
		t.Errorf("expected build.started, got %q", events[0].EventType)
	}
}

func TestPostgresEventStore_Prune_ExecError(t *testing.T) {
	ee := &fakeExecQueryExecutor{execErr: errors.New("db down")}
	qe := &fakeQueryExecutor{}
	store := NewPostgresEventStore(qe, ee)

	_, err := store.Prune(context.Background(), 24*time.Hour)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPostgresEventStore_Prune_Success(t *testing.T) {
	ee := &fakeExecQueryExecutor{
		execResult: db.ExecResult{RowsAffected: 100}, // less than pruneBatchSize
	}
	qe := &fakeQueryExecutor{}
	store := NewPostgresEventStore(qe, ee)

	deleted, err := store.Prune(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 100 {
		t.Errorf("expected 100 deleted, got %d", deleted)
	}
}

func TestPostgresEventStore_Prune_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	ee := &fakeExecQueryExecutor{
		execResult: db.ExecResult{RowsAffected: int64(pruneBatchSize)},
	}
	qe := &fakeQueryExecutor{}
	store := NewPostgresEventStore(qe, ee)

	_, err := store.Prune(ctx, 24*time.Hour)
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestPostgresEventStore_Append_ScanError(t *testing.T) {
	rows := &fakeRows{
		items:   [][]any{{int64(1)}},
		scanErr: errors.New("scan failed"),
	}
	qe := &fakeQueryExecutor{queryRows: rows}
	ee := &fakeExecQueryExecutor{}
	store := NewPostgresEventStore(qe, ee)

	_, err := store.Append(context.Background(), "bld-1", "ev", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected scan error")
	}
}

func TestPostgresEventStore_Append_RowsError(t *testing.T) {
	rows := &fakeRows{
		items:  [][]any{{int64(1)}},
		errVal: errors.New("rows iteration error"),
	}
	qe := &fakeQueryExecutor{queryRows: rows}
	ee := &fakeExecQueryExecutor{}
	store := NewPostgresEventStore(qe, ee)

	_, err := store.Append(context.Background(), "bld-1", "ev", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected rows error")
	}
}

// capturingQuery records the SQL and arguments of the last Query.
type capturingQuery struct {
	sql  string
	args []any
}

func (c *capturingQuery) Query(_ context.Context, sql string, args ...any) (db.Rows, error) {
	c.sql, c.args = sql, args
	return &fakeRows{}, nil
}

func (c *capturingQuery) QueryRow(context.Context, string, ...any) db.Row { return nil }

// signal.changed rows are keyed by RELEASE id, so they are reachable only
// through releases, and a stream for one environment must read only that
// environment's signals.
func TestPostgresEventStore_GetSignalsAfter_ReadsOneEnvironmentsSignalsThroughReleases(t *testing.T) {
	q := &capturingQuery{}
	store := NewPostgresEventStore(q, &fakeExecQueryExecutor{})

	if _, err := store.GetSignalsAfter(context.Background(), "tn-1", "shop", "staging", 5); err != nil {
		t.Fatalf("GetSignalsAfter: %v", err)
	}
	for _, want := range []string{"JOIN releases r ON r.id = se.build_id", "r.environment = $3", "se.event_type = 'signal.changed'", "r.tenant_id = $1"} {
		if !strings.Contains(q.sql, want) {
			t.Errorf("query lacks %q:\n%s", want, q.sql)
		}
	}
	if len(q.args) != 4 || q.args[0] != "tn-1" || q.args[1] != "shop" || q.args[2] != "staging" || q.args[3] != int64(5) {
		t.Errorf("unexpected args %v", q.args)
	}
}
