// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

// wire_project_cap_test.go — M5 (#106) regression guard for the cap-wiring helper.
// #106's root cause was TWO divergent wiring sites (the API store was capped, the
// build-orchestrator store was not). wireProjectCap is now the single path BOTH
// sites use; this test pins that the helper actually installs the checker (and
// that a nil checker leaves the store unenforced), so a regression of the wiring
// is caught here rather than silently reopening the deploy-path bypass.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// stubCapChecker is a fake store.ProjectCapChecker with a fixed bounded cap.
type stubCapChecker struct{ cap int }

func (s stubCapChecker) CheckProjectsValue(_ context.Context, _ string, _ int) (bool, string, int, error) {
	return false, "starter", s.cap, nil
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
	wireProjectCap(store, projectCapEnforcement{checker: stubCapChecker{cap: 1}}) // bounded → enforcement intended

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
	wireProjectCap(store, projectCapEnforcement{}) // no checker → no enforcement

	// With no checker, EnsureProject takes the original (unenforced) insert path,
	// which fails on the non-SqlcBridge handle — but NOT with the cap "pgx pool"
	// fail-closed error. The distinct failure proves the cap path was not taken.
	err := store.EnsureProject(context.Background(), nil, "tenant-1", "newproj", "user-1")
	if err != nil && strings.Contains(err.Error(), "pgx pool") {
		t.Fatalf("a nil checker must NOT engage the cap path; got cap error %v", err)
	}
}

// scriptedRow answers one QueryRow with a fixed value or error.
type scriptedRow struct {
	value any
	err   error
}

func (r scriptedRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	switch d := dest[0].(type) {
	case *string:
		*d = r.value.(string)
	case *int:
		*d = r.value.(int)
	}
	return nil
}

// deployTx plays the caller's (tenant-scoped, RLS-filtered) transaction on the
// deploy path: it resolves the account, grants the advisory lock, reports the
// project as new, and — being scoped to one tenant — counts only 0 projects.
type deployTx struct{ nonPgxDBTX }

func (deployTx) QueryRow(_ context.Context, sql string, _ ...any) db.Row {
	switch {
	case strings.Contains(sql, "SELECT account_id FROM tenants"):
		return scriptedRow{value: "acct-1"}
	case strings.Contains(sql, "SELECT 1 FROM projects"):
		return scriptedRow{err: db.ErrNoRows}
	default:
		return scriptedRow{value: 0}
	}
}

// accountWideCounter plays the cross-tenant connection: it sees every project
// the Account holds across its tenants.
type accountWideCounter struct {
	nonPgxDBTX
	projects int
}

func (c accountWideCounter) QueryRow(context.Context, string, ...any) db.Row {
	return scriptedRow{value: c.projects}
}

func TestWireProjectCap_DeployCountsTheAccountAcrossItsTenants(t *testing.T) {
	store := &store.PostgresProjectStore{DB: nonPgxDBTX{}}
	wireProjectCap(store, projectCapEnforcement{
		checker:     stubCapChecker{cap: 2},
		crossTenant: accountWideCounter{projects: 2},
	})

	// The deploying tenant's own transaction sees 0 projects; the Account holds 2
	// across its other tenants. A cap of 2 must refuse a third project — which it
	// only does if the count went through the cross-tenant connection.
	err := store.EnsureProject(context.Background(), deployTx{}, "tenant-1", "newproj", "user-1")
	var te *loopruntimeerrors.LoopRuntimeError
	if !errors.As(err, &te) || te.Code != "PROJECTS_EXCEED_PLAN" {
		t.Fatalf("a deploy past the per-Account cap must be refused with PROJECTS_EXCEED_PLAN; got %v", err)
	}
}
