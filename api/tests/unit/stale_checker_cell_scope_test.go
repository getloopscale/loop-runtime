// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/services/build"

	"github.com/getloopscale/loopzero/api/internal/services/release"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// Roadmap 03 — the reapers must scope their Find* queries to a single cell.
// These tests verify the SQL emitted by store.PostgresStaleScanner, behind the
// build and release stale checkers, includes `cell_id = $N` and binds the caller's
// cellID to that parameter.
//
// The fake DBTX captures SQL + args; nothing hits a real Postgres.

type capturingDBTX struct {
	lastSQL  string
	lastArgs []any
}

func (c *capturingDBTX) Query(_ context.Context, sql string, args ...any) (db.Rows, error) {
	c.lastSQL = sql
	c.lastArgs = args
	return &emptyDBRows{}, nil
}

func (c *capturingDBTX) Exec(_ context.Context, _ string, _ ...any) (db.ExecResult, error) {
	return db.ExecResult{}, nil
}

func (c *capturingDBTX) QueryRow(_ context.Context, _ string, _ ...any) db.Row {
	return emptyDBRow{}
}

type emptyDBRows struct{}

func (emptyDBRows) Next() bool          { return false }
func (emptyDBRows) Scan(_ ...any) error { return nil }
func (emptyDBRows) Err() error          { return nil }
func (emptyDBRows) Close()              {}

type emptyDBRow struct{}

func (emptyDBRow) Scan(_ ...any) error { return db.ErrNoRows }

func TestStaleBuildChecker_FindStaleBuilds_ScopedByCellID(t *testing.T) {
	cap := &capturingDBTX{}
	checker := staleBuildChecker(cap)

	_, err := checker.FindStaleBuilds(
		context.Background(),
		"cell-nyc3-01",
		5*time.Minute, 30*time.Minute, 10*time.Minute, 60*time.Second,
	)
	if err != nil {
		t.Fatalf("FindStaleBuilds: %v", err)
	}

	if !strings.Contains(cap.lastSQL, "cell_id = $5") {
		t.Errorf("expected SQL to contain 'cell_id = $5', got:\n%s", cap.lastSQL)
	}
	if !strings.Contains(cap.lastSQL, "'QUEUED'") || !strings.Contains(cap.lastSQL, "'RELEASING'") {
		t.Errorf("SQL must still include all stale states, got:\n%s", cap.lastSQL)
	}

	// The cell_id is the 5th positional parameter (timestamps are $1..$4).
	if len(cap.lastArgs) != 5 {
		t.Fatalf("expected 5 args, got %d: %v", len(cap.lastArgs), cap.lastArgs)
	}
	if cap.lastArgs[4] != "cell-nyc3-01" {
		t.Errorf("expected arg[4] == 'cell-nyc3-01', got %v", cap.lastArgs[4])
	}
}

func TestStaleBuildChecker_EmptyCellIDDefaultsToCellDefault(t *testing.T) {
	cap := &capturingDBTX{}
	checker := staleBuildChecker(cap)

	_, _ = checker.FindStaleBuilds(context.Background(), "", time.Minute, time.Minute, time.Minute, time.Minute)
	if cap.lastArgs[4] != db.CellDefault {
		t.Errorf("empty cellID should default to %q, got %v", db.CellDefault, cap.lastArgs[4])
	}
}

func TestStaleReleaseChecker_FindStaleReleases_ScopedByCellID(t *testing.T) {
	cap := &capturingDBTX{}
	checker := staleReleaseChecker(cap)

	_, err := checker.FindStaleReleases(
		context.Background(),
		"cell-sfo3-02",
		5*time.Minute, 10*time.Minute,
	)
	if err != nil {
		t.Fatalf("FindStaleReleases: %v", err)
	}

	if !strings.Contains(cap.lastSQL, "cell_id = $3") {
		t.Errorf("expected SQL to contain 'cell_id = $3', got:\n%s", cap.lastSQL)
	}
	if !strings.Contains(cap.lastSQL, "'READY'") || !strings.Contains(cap.lastSQL, "'PENDING'") {
		t.Errorf("SQL must still include READY and PENDING, got:\n%s", cap.lastSQL)
	}

	if len(cap.lastArgs) != 3 {
		t.Fatalf("expected 3 args, got %d: %v", len(cap.lastArgs), cap.lastArgs)
	}
	if cap.lastArgs[2] != "cell-sfo3-02" {
		t.Errorf("expected arg[2] == 'cell-sfo3-02', got %v", cap.lastArgs[2])
	}
}

// rowsDBTX returns fixed rows so the status → timeout-event mapping can be
// observed end to end.
type rowsDBTX struct {
	capturingDBTX
	rows [][]string
}

func (r *rowsDBTX) Query(ctx context.Context, sql string, args ...any) (db.Rows, error) {
	_, _ = r.capturingDBTX.Query(ctx, sql, args...)
	return &fixedRows{rows: r.rows, i: -1}, nil
}

type fixedRows struct {
	rows [][]string
	i    int
}

func (f *fixedRows) Next() bool { f.i++; return f.i < len(f.rows) }
func (f *fixedRows) Scan(dest ...any) error {
	for k := range dest {
		*(dest[k].(*string)) = f.rows[f.i][k]
	}
	return nil
}
func (f *fixedRows) Err() error { return nil }
func (f *fixedRows) Close()     {}

func TestStaleReleaseChecker_MapsStatusToTimeoutEventAndSkipsOthers(t *testing.T) {
	src := &rowsDBTX{rows: [][]string{
		{"rel-1", "READY", "tn", "p", "production", "b1"},
		{"rel-2", "PENDING", "tn", "p", "production", "b2"},
		{"rel-3", "ACTIVE", "tn", "p", "production", "b3"},
	}}

	got, err := staleReleaseChecker(src).FindStaleReleases(context.Background(), "", time.Minute, time.Minute)

	if err != nil {
		t.Fatalf("FindStaleReleases: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 stale releases (ACTIVE skipped), got %+v", got)
	}
	if got[0].ID != "rel-1" || got[0].Event != statemachine.RPEventGitPushTimeout || got[0].BuildID != "b1" {
		t.Errorf("READY row mapped wrong: %+v", got[0])
	}
	if got[1].ID != "rel-2" || got[1].Event != statemachine.RPEventSyncTimeout || got[1].Environment != "production" {
		t.Errorf("PENDING row mapped wrong: %+v", got[1])
	}
	if src.lastArgs[2] != db.CellDefault {
		t.Errorf("empty cell must default to %q, got %v", db.CellDefault, src.lastArgs[2])
	}
}

func TestStaleBuildChecker_MapsStatusToTimeoutEventAndSkipsOthers(t *testing.T) {
	src := &rowsDBTX{rows: [][]string{
		{"b-1", "QUEUED"},
		{"b-2", "RUNNING"},
		{"b-3", "SUCCEEDED"},
	}}

	got, err := staleBuildChecker(src).FindStaleBuilds(context.Background(), "", time.Minute, time.Minute, time.Minute, time.Minute)

	if err != nil {
		t.Fatalf("FindStaleBuilds: %v", err)
	}
	if len(got) != 2 || got[0].ID != "b-1" || got[1].ID != "b-2" {
		t.Fatalf("want b-1 and b-2 (SUCCEEDED skipped), got %+v", got)
	}
	if got[0].State != statemachine.BuildPhase("QUEUED") || got[0].Event == "" || got[1].Event == "" {
		t.Errorf("timeout events not mapped: %+v", got)
	}
}

// The production composition: the store's scanner behind the reaper's port.
func staleReleaseChecker(dbtx db.DBTX) release.ReleaseStaleChecker {
	return release.NewStaleReleaseChecker(&store.PostgresStaleScanner{DB: dbtx})
}

func staleBuildChecker(dbtx db.DBTX) build.BuildStaleChecker {
	return build.NewStaleBuildChecker(&store.PostgresStaleScanner{DB: dbtx})
}
