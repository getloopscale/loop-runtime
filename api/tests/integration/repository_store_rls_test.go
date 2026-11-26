// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/dbtx"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
)

// A GitHub installation is a global object that a tenant claims, and a
// repository link is keyed globally by repo_full_name: the webhook arrives with
// no tenant, and the API's authorization gates ask "which tenant owns this
// installation / this repository?" before letting the caller act. Both
// questions span tenants. Over the request pool, scoped to the caller, another
// tenant's installation is hidden — the owner check saw nothing and let the
// caller list that installation's repositories. These tests pin which
// connection can answer — the composition root builds the repository store on
// the cross-tenant pool.
//
//	DATABASE_URL=<loopzero_api> LOOPZERO_WORKER_DATABASE_URL=<loopzero_worker> \
//	  go test ./tests/integration/ -run RepositoryStore -v
const (
	repoOwnerTenant, repoOtherTenant = "tn-rls-repo-a", "tn-rls-repo-b"
	repoInstallationID               = int64(987654321)
)

func repositorySeed(t *testing.T) {
	t.Helper()
	const owner = "rls-repo-uid"
	_ = testPool(t) // skips when PostgreSQL is unreachable
	seed, err := sql.Open("pgx", seedDBURL())
	if err != nil {
		t.Fatalf("open seed DB: %v", err)
	}
	t.Cleanup(func() { _ = seed.Close() })
	projectCapSeed(t, seed, owner, "acct-rls-repo", repoOwnerTenant, repoOtherTenant, "starter")
	cleanup := func() {
		if _, err := seed.Exec(`DELETE FROM github_installations WHERE id = $1`, repoInstallationID); err != nil {
			t.Errorf("cleanup installation: %v", err)
		}
	}
	cleanup()
	t.Cleanup(func() { cleanup(); projectCapCleanup(t, seed, owner) })
	if _, err := seed.Exec(`INSERT INTO github_installations (id, account_login, tenant_id) VALUES ($1, 'acme', $2)`,
		repoInstallationID, repoOwnerTenant); err != nil {
		t.Fatalf("seed installation: %v", err)
	}
}

func TestRepositoryStoreOnTheWorkerRoleSeesTheInstallationsOwner(t *testing.T) {
	repositorySeed(t)
	repos := &store.PostgresRepositoryStore{DB: &db.PgxAdapter{Pool: workerPool(t)}}

	inst, err := repos.GetInstallation(context.Background(), repoInstallationID)

	if err != nil || inst == nil || inst.TenantID == nil || *inst.TenantID != repoOwnerTenant {
		t.Fatalf("installation over the worker role = %+v, %v; want owned by %s", inst, err, repoOwnerTenant)
	}
}

// The failure the cross-tenant wiring prevents: scoped to another tenant, the
// request pool hides the installation, and "no owner" read as "not claimed".
func TestRepositoryStoreOnTheRequestPoolHidesAnotherTenantsInstallation(t *testing.T) {
	repositorySeed(t)
	repos := &store.PostgresRepositoryStore{DB: scopedRequestPool(t)}

	inst, err := repos.GetInstallation(dbtx.WithTenantScope(context.Background(), repoOtherTenant), repoInstallationID)

	if err != nil || inst != nil {
		t.Fatalf("installation over the request pool as %s = %+v, %v; want hidden (nil)", repoOtherTenant, inst, err)
	}
}
