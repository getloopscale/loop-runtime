// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package integration

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/release"
)

// The admin routes act on a tenant the caller does not belong to: an admin
// call is authorized by service auth and carries no tenant, so on the request
// pool (scoped to the caller, i.e. to nobody) fail-closed RLS hides every row.
// Eviction matched and deleted nothing while the cluster side was torn down,
// the pin could not find the tenant or count the target cell, and the release
// inspector found no release (issue #386). These tests pin which connection
// can answer — the composition root builds all three on the cross-tenant pool.
//
//	DATABASE_URL=<loopzero_api> LOOPZERO_WORKER_DATABASE_URL=<loopzero_worker> \
//	LOOPZERO_OWNER_DATABASE_URL=<loopzero> go test ./tests/integration/ -run Admin -v
const (
	adminOwner   = "rls-admin-uid"
	adminTenantA = "tn-rls-admin-a"
	adminTenantB = "tn-rls-admin-b"
	adminCell    = "cell-rls-admin-pin"
)

func adminSeed(t *testing.T) *sql.DB {
	t.Helper()
	seed := openSeed(t)
	projectCapSeed(t, seed, adminOwner, "acct-rls-admin", adminTenantA, adminTenantB, "starter")
	t.Cleanup(func() {
		projectCapCleanup(t, seed, adminOwner)
		if _, err := seed.Exec(`DELETE FROM cells WHERE cell_id = $1`, adminCell); err != nil {
			t.Errorf("cleanup cell: %v", err)
		}
	})
	return seed
}

func mustExecSeed(t *testing.T, seed *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := seed.Exec(query, args...); err != nil {
		t.Fatalf("seed %q: %v", query, err)
	}
}

func TestAdminEvictionOnTheWorkerRoleRemovesTheTenantAndItsMeteringCursors(t *testing.T) {
	requireTheWorkerRolesOwnPrivileges(t)
	seed := adminSeed(t)
	// metering_cursor has no foreign key to tenants: the tenant cleanup does not
	// reach it, so the fixture removes its own cursor on both ends.
	const dropCursor = `DELETE FROM metering_cursor WHERE tenant_id = $1`
	mustExecSeed(t, seed, dropCursor, adminTenantA)
	t.Cleanup(func() {
		if _, err := seed.Exec(dropCursor, adminTenantA); err != nil {
			t.Errorf("cleanup metering cursor: %v", err)
		}
	})
	mustExecSeed(t, seed, `INSERT INTO metering_cursor (tenant_id, metric, cell_id, last_metered_at)
		VALUES ($1, 'cpu_core_seconds', 'cell-0', now())`, adminTenantA)
	evictions := store.NewTenantStoreSqlc(&db.PgxAdapter{Pool: workerPool(t)})
	ctx := context.Background()

	matched, err := evictions.ListTenantIDsByPattern(ctx, "tn-rls-admin-%")
	if err != nil || len(matched) != 2 {
		t.Fatalf("pattern match over the worker role = %v, %v; want both tenants", matched, err)
	}
	counts, err := evictions.DeleteTenantsFKOrdered(ctx, []string{adminTenantA})

	if err != nil || counts.Tenants != 1 || counts.MeteringCursors != 1 {
		t.Fatalf("eviction over the worker role = %+v, %v; want the tenant and its cursor removed", counts, err)
	}
}

// The failure the cross-tenant wiring prevents: the admin request has no
// tenant scope, so the pattern matches nothing and the cascade deletes nothing
// — while the evictor goes on to tear the cluster side down.
func TestAdminEvictionOnTheRequestPoolMatchesAndDeletesNothing(t *testing.T) {
	adminSeed(t)
	evictions := store.NewTenantStoreSqlc(scopedRequestPool(t))
	ctx := context.Background()

	matched, matchErr := evictions.ListTenantIDsByPattern(ctx, "tn-rls-admin-%")
	counts, deleteErr := evictions.DeleteTenantsFKOrdered(ctx, []string{adminTenantA})

	if matchErr != nil || len(matched) != 0 || deleteErr != nil || counts.Tenants != 0 {
		t.Fatalf("over the request pool: matched %v (%v), deleted %+v (%v); want nothing (hidden by RLS)",
			matched, matchErr, counts, deleteErr)
	}
}

// The pin's capacity check counts every tenant on the target cell. With
// capacity for one and another tenant already there, only a connection that
// sees that other tenant refuses the pin.
func TestAdminPinOnTheWorkerRoleCountsTheOtherTenantsOfTheCell(t *testing.T) {
	seed := adminSeed(t)
	mustExecSeed(t, seed, `INSERT INTO cells (cell_id, display_name, capacity_tenants_max) VALUES ($1, 'rls pin', 1)`, adminCell)
	mustExecSeed(t, seed, `UPDATE tenants SET cell_id = $1 WHERE id = $2`, adminCell, adminTenantB)
	cells := &store.PostgresCellStore{DB: &db.PgxAdapter{Pool: workerPool(t)}}

	_, err := cells.PinTenantToCell(context.Background(), adminTenantA, adminCell)

	if !errors.Is(err, store.ErrCellPinnerAtCapacity) {
		t.Fatalf("pin into a full cell over the worker role = %v; want at capacity", err)
	}
}

func TestAdminPinOnTheWorkerRoleMovesTheTenant(t *testing.T) {
	seed := adminSeed(t)
	mustExecSeed(t, seed, `INSERT INTO cells (cell_id, display_name) VALUES ($1, 'rls pin')`, adminCell)
	cells := &store.PostgresCellStore{DB: &db.PgxAdapter{Pool: workerPool(t)}}

	previous, err := cells.PinTenantToCell(context.Background(), adminTenantA, adminCell)

	if err != nil || previous != "cell-0" {
		t.Fatalf("pin over the worker role = %q, %v; want it moved off cell-0", previous, err)
	}
}

func TestAdminPinOnTheRequestPoolCannotFindTheTenant(t *testing.T) {
	seed := adminSeed(t)
	mustExecSeed(t, seed, `INSERT INTO cells (cell_id, display_name) VALUES ($1, 'rls pin')`, adminCell)
	cells := &store.PostgresCellStore{DB: scopedRequestPool(t)}

	_, err := cells.PinTenantToCell(context.Background(), adminTenantA, adminCell)

	if !errors.Is(err, store.ErrCellPinnerTenantNotFound) {
		t.Fatalf("pin over the request pool = %v; want tenant not found (hidden by RLS)", err)
	}
}

func adminSeedRelease(t *testing.T, seed *sql.DB) string {
	t.Helper()
	const build, rel = "bld-rls-admin-inspect", "rel-rls-admin-inspect"
	mustExecSeed(t, seed, `INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
		VALUES ($1, $2, 'p', 'production', 'SUCCEEDED', 'c', $3)`, build, adminTenantA, adminOwner)
	mustExecSeed(t, seed, `INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by)
		VALUES ($1, $2, $3, 'p', 'production', 'PENDING', 'c', $4)`, rel, build, adminTenantA, adminOwner)
	t.Cleanup(func() {
		if _, err := seed.Exec(`DELETE FROM releases WHERE id = $1`, rel); err != nil {
			t.Errorf("cleanup release: %v", err)
		}
		if _, err := seed.Exec(`DELETE FROM builds WHERE id = $1`, build); err != nil {
			t.Errorf("cleanup build: %v", err)
		}
	})
	return rel
}

func adminInspectorOn(pool db.DBTX) release.ReleaseInspector {
	return release.NewReleaseInspector(store.NewReleaseStoreSqlc(pool, store.NoopEventStore{}), &store.PostgresAuditStore{DB: pool}, nil)
}

func TestAdminReleaseInspectionOnTheWorkerRoleFindsTheRelease(t *testing.T) {
	rel := adminSeedRelease(t, adminSeed(t))

	got, err := adminInspectorOn(&db.PgxAdapter{Pool: workerPool(t)}).Inspect(context.Background(), rel)

	if err != nil || got.TenantID != adminTenantA {
		t.Fatalf("inspection over the worker role = %+v, %v; want the release of %s", got, err, adminTenantA)
	}
}

func TestAdminReleaseInspectionOnTheRequestPoolFindsNoRelease(t *testing.T) {
	rel := adminSeedRelease(t, adminSeed(t))

	_, err := adminInspectorOn(scopedRequestPool(t)).Inspect(context.Background(), rel)

	// Hidden by RLS reads as not found; the inspector reports its own not-found,
	// which the admin handler answers with 404 (#415).
	var notFound release.ErrReleaseInspectionNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("inspection over the request pool = %v; want the release not found (hidden by RLS)", err)
	}
}
