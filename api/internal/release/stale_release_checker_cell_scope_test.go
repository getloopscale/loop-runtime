// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/release"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// Roadmap 03 — the reapers must scope their Find* queries to a single cell.
// These tests verify the SQL emitted by store.PostgresStaleScanner, behind the
// release stale checker, includes `cell_id = $N` and binds the caller's
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

func (emptyDBRows) Next() bool { return false }

func (emptyDBRows) Scan(_ ...any) error { return nil }

func (emptyDBRows) Err() error { return nil }

func (emptyDBRows) Close() {}

type emptyDBRow struct{}

func (emptyDBRow) Scan(_ ...any) error { return db.ErrNoRows }

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

func (f *fixedRows) Close() {}

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
	// An empty cell means every cell (backgroundloops.Deps: "Empty means every cell").
	// It used to become cell-0, so the API — which runs the loops with no cell —
	// never reaped a release on any other cell (#447).
	if src.lastArgs[2] != "" {
		t.Errorf("empty cell must scan every cell, got cell arg %v", src.lastArgs[2])
	}
	if !strings.Contains(src.lastSQL, "$3 = ''") {
		t.Errorf("the query does not treat an empty cell as every cell:\n%s", src.lastSQL)
	}
}

// The production composition: the store's scanner behind the reaper's port.
func staleReleaseChecker(dbtx db.DBTX) release.ReleaseStaleChecker {
	return release.NewStaleReleaseChecker(&store.PostgresStaleScanner{DB: dbtx})
}
