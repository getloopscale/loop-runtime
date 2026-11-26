// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/config"
	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// fakeDBTX implements db.DBTX for controller-local tests without a real
// Postgres. The tests in main_test.go only exercise QueryRow via
// leader.Acquirer.Validate; Exec and Query are stubbed.
type fakeDBTX struct {
	row fakeRow
}

func (f *fakeDBTX) Query(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	return nil, nil
}

func (f *fakeDBTX) Exec(_ context.Context, _ string, _ ...any) (db.ExecResult, error) {
	return db.ExecResult{}, nil
}

func (f *fakeDBTX) QueryRow(_ context.Context, _ string, _ ...any) db.Row {
	return f.row
}

// fakeRow is a db.Row whose Scan invokes a caller-supplied function. The
// tests use this to inject lease state (generation + holder) into the
// leader.Acquirer.Validate path.
type fakeRow struct {
	scan func(dest ...any) error
}

func (r fakeRow) Scan(dest ...any) error { return r.scan(dest...) }

// stubConfig returns a zero-value config.Config. The cellID helper only
// reads the LOOP_RUNTIME_CELL_ID env var so the config value is unused; this
// exists to keep the test call sites readable.
func stubConfig() config.Config { return config.Config{} }
