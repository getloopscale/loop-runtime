// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/services"
)

// Roadmap 03 — the reapers must scope their Find* queries to a single cell.
// These tests verify the SQL emitted by PostgresStaleBuildChecker and
// PostgresStaleReleaseChecker includes `cell_id = $N` and binds the caller's
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

func (emptyDBRows) Next() bool             { return false }
func (emptyDBRows) Scan(_ ...any) error    { return nil }
func (emptyDBRows) Err() error             { return nil }
func (emptyDBRows) Close()                 {}

type emptyDBRow struct{}

func (emptyDBRow) Scan(_ ...any) error { return db.ErrNoRows }

func TestPostgresStaleBuildChecker_FindStaleBuilds_ScopedByCellID(t *testing.T) {
	cap := &capturingDBTX{}
	checker := &services.PostgresStaleBuildChecker{DB: cap}

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

func TestPostgresStaleBuildChecker_EmptyCellIDDefaultsToCellDefault(t *testing.T) {
	cap := &capturingDBTX{}
	checker := &services.PostgresStaleBuildChecker{DB: cap}

	_, _ = checker.FindStaleBuilds(context.Background(), "", time.Minute, time.Minute, time.Minute, time.Minute)
	if cap.lastArgs[4] != db.CellDefault {
		t.Errorf("empty cellID should default to %q, got %v", db.CellDefault, cap.lastArgs[4])
	}
}

func TestPostgresStaleReleaseChecker_FindStaleReleases_ScopedByCellID(t *testing.T) {
	cap := &capturingDBTX{}
	checker := &services.PostgresStaleReleaseChecker{DB: cap}

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
