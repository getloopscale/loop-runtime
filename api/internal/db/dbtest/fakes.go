// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package dbtest holds the shared DBTX test fakes used by the db-store adapter
// tests.
//
// M9 Step 5 (slice 5a.5): these fakes (FakeDBTX, RecordingDBTX, and the row
// stubs) were unexported types in the flat internal/db test files
// (stores_test.go, cell_scoped_queries_test.go). Relocating them to an exported
// leaf package — importable by BOTH the root db tests and the db/store adapter
// tests — is the prerequisite the design review (MEET-2026-07-03-001) identified
// for moving the remaining entity adapters (whose tests share these fakes) into
// db/store/ without duplicating the mock ecosystem. Behavior-preserving:
// verbatim logic, exported names/fields.
// M9 5c batch 4 (ADR 2026-07-03-m9-db-store-subpackage-topology D4): these
// exported DBTX test doubles were extracted OUT of the production db package
// (formerly internal/db/fakes.go) into this dedicated dbtest support package,
// so the production db package no longer ships test doubles. Consumed by the
// db/store adapter tests and the root-db external `package db_test` files
// (cell_scoped_queries_test, stores_extra_test).
// Contained to internal/, no runtime path.
// Package dbtest provides in-memory DBTX test doubles shared by db-adapter unit tests. M9 Step 5.
package dbtest

import (
	"context"
	"strings"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

var (
	_ db.DBTX = (*FakeDBTX)(nil)
	_ db.DBTX = (*RecordingDBTX)(nil)
)

// FakeDBTX implements DBTX with configurable responses per call.
type FakeDBTX struct {
	ExecResult     db.ExecResult
	ExecErr        error
	QueryRows      db.Rows
	QueryErr       error
	QueryRowResult db.Row
	CapturedSQL    string
	CapturedArgs   []any
	ExecCalls      int
}

func (f *FakeDBTX) Query(_ context.Context, sql string, args ...any) (db.Rows, error) {
	f.CapturedSQL = sql
	f.CapturedArgs = args
	if f.QueryErr != nil {
		return nil, f.QueryErr
	}
	if f.QueryRows != nil {
		return f.QueryRows, nil
	}
	return &SliceRows{}, nil
}

func (f *FakeDBTX) Exec(_ context.Context, sql string, args ...any) (db.ExecResult, error) {
	f.CapturedSQL = sql
	f.CapturedArgs = args
	f.ExecCalls++
	return f.ExecResult, f.ExecErr
}

func (f *FakeDBTX) QueryRow(_ context.Context, sql string, args ...any) db.Row {
	f.CapturedSQL = sql
	f.CapturedArgs = args
	if f.QueryRowResult != nil {
		return f.QueryRowResult
	}
	return EmptyRow{}
}

// RecordingDBTX is a DBTX fake that captures queries without executing them.
type RecordingDBTX struct {
	LastSQL        string
	LastArgs       []any
	NextRows       db.Rows
	FilterByCellID bool
	Dataset        []FakeRow
}

func (r *RecordingDBTX) Query(_ context.Context, sql string, args ...any) (db.Rows, error) {
	r.LastSQL = sql
	r.LastArgs = args
	if r.FilterByCellID {
		var wanted string
		for _, a := range args {
			if s, ok := a.(string); ok && strings.HasPrefix(s, "cell-") {
				wanted = s
				break
			}
		}
		var matched []FakeRow
		for _, row := range r.Dataset {
			if row.CellID == wanted {
				matched = append(matched, row)
			}
		}
		return &SliceRows{Rows: matched}, nil
	}
	if r.NextRows != nil {
		return r.NextRows, nil
	}
	return &SliceRows{}, nil
}

func (r *RecordingDBTX) Exec(_ context.Context, _ string, _ ...any) (db.ExecResult, error) {
	return db.ExecResult{}, nil
}

func (r *RecordingDBTX) QueryRow(_ context.Context, _ string, _ ...any) db.Row {
	return EmptyRow{}
}

// FakeRow is a pre-built fake row with an owning cellID (for cell-scoped
// isolation tests) and positional scan values.
type FakeRow struct {
	CellID string
	Values []any
}

// EmptyRow is a Row that always reports no rows.
type EmptyRow struct{}

func (EmptyRow) Scan(_ ...any) error { return db.ErrNoRows }

// SliceRows returns a slice of pre-built fake rows through the Rows iterator.
type SliceRows struct {
	Rows []FakeRow
	Idx  int
}

func (s *SliceRows) Next() bool {
	if s.Idx >= len(s.Rows) {
		return false
	}
	s.Idx++
	return true
}

func (s *SliceRows) Scan(dest ...any) error {
	row := s.Rows[s.Idx-1]
	for i, d := range dest {
		if i >= len(row.Values) {
			return nil
		}
		switch target := d.(type) {
		case *string:
			if v, ok := row.Values[i].(string); ok {
				*target = v
			}
		}
	}
	return nil
}

func (s *SliceRows) Err() error { return nil }
func (s *SliceRows) Close()     {}
