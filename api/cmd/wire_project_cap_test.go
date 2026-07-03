// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

// wire_project_cap_test.go — M5 (#106) regression guard for the cap-wiring helper.
// #106's root cause was TWO divergent wiring sites (the API store was capped, the
// build-orchestrator store was not). wireProjectCap is now the single path BOTH
// sites use; this test pins that the helper actually installs the checker (and
// that a nil checker leaves the store unenforced), so a regression of the wiring
// is caught here rather than silently reopening the deploy-path bypass.

import (
	"context"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/db"
)

// stubCapChecker is a fake store.ProjectCapChecker with a fixed bounded cap.
type stubCapChecker struct{ cap int }

func (s stubCapChecker) CheckProjectsValue(_ string, _ int) (bool, string, int) {
	return false, "starter", s.cap
}

// nonPgxDBTX is a minimal db.DBTX that is NOT a *db.PgxAdapter, so the cap path
// (which needs a pgx pool to hold the advisory lock) fails closed on it.
type nonPgxDBTX struct{}

func (nonPgxDBTX) Query(context.Context, string, ...any) (db.Rows, error) { return nil, nil }
func (nonPgxDBTX) Exec(context.Context, string, ...any) (db.ExecResult, error) {
	return db.ExecResult{}, nil
}
func (nonPgxDBTX) QueryRow(context.Context, string, ...any) db.Row { return nil }

func TestWireProjectCap_WiresCheckerWhenPresent(t *testing.T) {
	store := &store.PostgresProjectStore{DB: nonPgxDBTX{}}
	wireProjectCap(store, stubCapChecker{cap: 1}) // bounded → enforcement intended

	// With the checker wired and a bounded cap, a deploy on a non-pgx handle (no
	// pool to hold the advisory lock) MUST fail closed — proving the checker is
	// actually installed (without it, EnsureProject would take the unenforced
	// insert path instead).
	err := store.EnsureProject(context.Background(), nil, "tenant-1", "newproj", "user-1")
	if err == nil || !strings.Contains(err.Error(), "pgx pool") {
		t.Fatalf("a wired bounded checker must fail closed without a pgx pool; got %v", err)
	}
}

func TestWireProjectCap_NilCheckerLeavesStoreUnenforced(t *testing.T) {
	store := &store.PostgresProjectStore{DB: nonPgxDBTX{}}
	wireProjectCap(store, nil) // no checker → no enforcement

	// With no checker, EnsureProject takes the original (unenforced) insert path,
	// which fails on the non-SqlcBridge handle — but NOT with the cap "pgx pool"
	// fail-closed error. The distinct failure proves the cap path was not taken.
	err := store.EnsureProject(context.Background(), nil, "tenant-1", "newproj", "user-1")
	if err != nil && strings.Contains(err.Error(), "pgx pool") {
		t.Fatalf("a nil checker must NOT engage the cap path; got cap error %v", err)
	}
}
