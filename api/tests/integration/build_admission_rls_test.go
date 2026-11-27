// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/builds/buildsadapter"
	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/dbtx"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
)

// The build admission gate caps concurrent builds PER CELL, so it counts the
// active builds of every tenant placed on the cell. builds is under fail-closed
// Row Level Security: over the request pool, scoped to the caller, the count
// saw only the caller's own builds and the per-cell cap never engaged. These
// tests pin which connection can count a cell — the composition root builds
// the admission querier on the cross-tenant pool.
//
//	DATABASE_URL=<loop_runtime_api> LOOP_RUNTIME_WORKER_DATABASE_URL=<loop_runtime_worker> \
//	  go test ./tests/integration/ -run AdmissionQuerier -v
const (
	admissionCell    = "cell-rls-admission"
	admissionTenantA = "tn-rls-adm-a"
	admissionTenantB = "tn-rls-adm-b"
)

func admissionSeed(t *testing.T) {
	t.Helper()
	const owner = "rls-adm-uid"
	_ = testPool(t) // skips when PostgreSQL is unreachable
	seed, err := sql.Open("pgx", seedDBURL())
	if err != nil {
		t.Fatalf("open seed DB: %v", err)
	}
	t.Cleanup(func() { _ = seed.Close() })
	projectCapSeed(t, seed, owner, "acct-rls-adm", admissionTenantA, admissionTenantB, "starter")
	cleanup := func() {
		if _, err := seed.Exec(`DELETE FROM builds WHERE cell_id = $1`, admissionCell); err != nil {
			t.Errorf("cleanup builds: %v", err)
		}
	}
	cleanup()
	t.Cleanup(func() { cleanup(); projectCapCleanup(t, seed, owner) })
	for _, tn := range []string{admissionTenantA, admissionTenantB} {
		if _, err := seed.Exec(`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by, cell_id)
			VALUES ($1, $2, 'p', 'production', 'RUNNING', 'c', $3, $4)`, "bld-"+tn, tn, owner, admissionCell); err != nil {
			t.Fatalf("seed a running build for %s: %v", tn, err)
		}
	}
}

func TestAdmissionQuerierOnTheWorkerRoleCountsEveryTenantOfTheCell(t *testing.T) {
	admissionSeed(t)
	querier := &buildsadapter.BuildStoreAdmissionQuerier{Store: store.NewBuildStoreSqlc(&db.PgxAdapter{Pool: workerPool(t)})}

	snap, err := querier.ActiveBuilds(context.Background(), admissionCell)

	if err != nil || snap.Total != 2 {
		t.Fatalf("active builds on the cell over the worker role = %+v, %v; want 2", snap, err)
	}
}

// The failure the cross-tenant wiring prevents: scoped to the caller, the
// request pool counts one tenant's builds and calls it the cell's load.
func TestAdmissionQuerierOnTheRequestPoolSeesOnlyTheCallersBuilds(t *testing.T) {
	admissionSeed(t)
	querier := &buildsadapter.BuildStoreAdmissionQuerier{Store: store.NewBuildStoreSqlc(scopedRequestPool(t))}

	snap, err := querier.ActiveBuilds(dbtx.WithTenantScope(context.Background(), admissionTenantA), admissionCell)

	if err != nil || snap.Total != 1 {
		t.Fatalf("active builds on the cell over the request pool = %+v, %v; want 1 (the caller's only)", snap, err)
	}
}
