// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package integration

import (
	"sync"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/ports"
)

// M5 T2.1 — the HERMETIC per-Account project cap on the DEPLOY path
// (EnsureProject), proven against real PostgreSQL. Closes the #106 bypass where
// `loopzero deploy <new-name>` created a project past the cap that M4 only enforced
// on POST /api/v1/projects. Reuses the M4 integration harness (projectCapStore / projectCapSeed /
// projectCapCleanup / violationCount / isCapError) from project_cap_per_account_test.go.
//
//	DATABASE_URL=postgres://loopzero:loopzero@host/db?sslmode=disable go test ./tests/integration/ -run ProjectCapOnDeploy -race -v

// mustEnsure drives the deploy-time auto-create (nil tx → EnsureProject opens its
// own tx and account-lock, the EC-1 own-tx branch). Mirrors what the build-submit
// path does, minus the env-lock (covered separately by the held-env-lock test).
func mustEnsure(t *testing.T, store *store.PostgresProjectStore, tenant, name, owner string) error {
	t.Helper()
	return store.EnsureProject(asTenant(tenant), nil, tenant, name, owner)
}

// Scenario 1 — a deploy of the FIRST new project (count→cap) is allowed
// (EC-2 boundary-inclusive); a deploy of a SECOND new project (under the other
// tenant of the same account) is rejected with PROJECTS_EXCEED_PLAN + metric++.
func TestProjectCapOnDeploy_DeployBlocksNewBeyondCap(t *testing.T) {
	store, std := projectCapStore(t)
	const owner, acct, tn1, tn2 = "m5-uid", "acct-m5", "tn-m5-a", "tn-m5-b"
	projectCapSeed(t, std, owner, acct, tn1, tn2, "starter") // cap = 1
	t.Cleanup(func() { projectCapCleanup(t, std, owner) })

	// EC-2: the FIRST new project brings count to exactly cap → allowed.
	if err := mustEnsure(t, store, tn1, "first", owner); err != nil {
		t.Fatalf("deploy of the first new project (count→cap) must succeed: %v", err)
	}

	before := violationCount("starter")
	// The SECOND new project, under the OTHER tenant of the same account → blocked.
	err := mustEnsure(t, store, tn2, "second", owner)
	if !isCapError(err) {
		t.Fatalf("deploy of a new project over the Account cap must be PROJECTS_EXCEED_PLAN; got %v", err)
	}
	if got := violationCount("starter"); got != before+1 {
		t.Fatalf("violation metric must increment by 1 on deploy rejection; before=%v after=%v", before, got)
	}
	// The blocked project must NOT have been inserted.
	var n int
	if err := std.QueryRow(`SELECT count(*) FROM projects WHERE tenant_id IN (SELECT id FROM tenants WHERE account_id=$1)`, acct).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("blocked deploy must not insert; count=%d want 1", n)
	}
}

// Scenario 2 — a re-deploy to an ALREADY-EXISTING project is never blocked, even
// when the account is grandfathered OVER the cap (block-new-only, ADR-M5-2).
func TestProjectCapOnDeploy_ExistingProjectRedeployNeverBlocked(t *testing.T) {
	store, std := projectCapStore(t)
	const owner, acct, tn1, tn2 = "m5-ex-uid", "acct-m5ex", "tn-m5ex-a", "tn-m5ex-b"
	projectCapSeed(t, std, owner, acct, tn1, tn2, "starter") // cap = 1
	t.Cleanup(func() { projectCapCleanup(t, std, owner) })

	// Grandfather TWO projects (over the cap of 1) directly.
	for _, p := range []string{"keep-1", "keep-2"} {
		if _, err := std.Exec(`INSERT INTO projects (id,tenant_id,name,created_by) VALUES ($1,$2,$3,$4)`, "proj-"+p, tn1, p, owner); err != nil {
			t.Fatalf("seed grandfathered: %v", err)
		}
	}
	before := violationCount("starter")

	// A re-deploy to an EXISTING project succeeds (never blocked) and is a no-op.
	if err := mustEnsure(t, store, tn1, "keep-1", owner); err != nil {
		t.Fatalf("re-deploy to an existing project must never be blocked: %v", err)
	}
	// A NEW project is still blocked (cap binds for new only).
	if err := mustEnsure(t, store, tn1, "brand-new", owner); !isCapError(err) {
		t.Fatalf("a NEW project on an over-cap account must be blocked; got %v", err)
	}
	// The existing-project re-deploy must NOT have incremented the metric.
	if got := violationCount("starter"); got != before+1 {
		// before+1 because exactly ONE rejection happened (the brand-new), not the re-deploy.
		t.Fatalf("only the new-project rejection should count; before=%v after=%v want before+1", before, got)
	}
	// Count unchanged at 2 (re-deploy no-op, new blocked).
	var n int
	if err := std.QueryRow(`SELECT count(*) FROM projects WHERE tenant_id IN (SELECT id FROM tenants WHERE account_id=$1)`, acct).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("re-deploy must be a no-op and new must be blocked; count=%d want 2", n)
	}
}

// Scenario 3 — enterprise (cap=NULL/unlimited): deploys never blocked, metric flat (EC-6).
func TestProjectCapOnDeploy_EnterpriseUnlimited(t *testing.T) {
	store, std := projectCapStore(t)
	const owner, acct, tn1, tn2 = "m5-ent-uid", "acct-m5ent", "tn-m5ent-a", "tn-m5ent-b"
	projectCapSeed(t, std, owner, acct, tn1, tn2, "enterprise")
	t.Cleanup(func() { projectCapCleanup(t, std, owner) })

	before := violationCount("enterprise")
	for i := 0; i < 5; i++ {
		if err := mustEnsure(t, store, tn1, "ent-"+string(rune('a'+i)), owner); err != nil {
			t.Fatalf("enterprise deploy must never block (project %d): %v", i, err)
		}
	}
	if got := violationCount("enterprise"); got != before {
		t.Fatalf("enterprise must not increment the violation metric; before=%v after=%v", before, got)
	}
}

// Scenario 4 — a deploy (EnsureProject) of a NEW project and an API CreateProject
// of ANOTHER new project for the SAME account at the boundary cannot both
// succeed: they serialize on the SAME hashtext(accountID) advisory lock.
func TestProjectCapOnDeploy_DeployAndApiCreateShareTheCap(t *testing.T) {
	store, std := projectCapStore(t)
	const owner, acct, tn1, tn2 = "m5-sh-uid", "acct-m5sh", "tn-m5sh-a", "tn-m5sh-b"
	projectCapSeed(t, std, owner, acct, tn1, tn2, "starter") // cap = 1, account empty → exactly one slot
	t.Cleanup(func() { projectCapCleanup(t, std, owner) })

	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, capRejected, otherErr := 0, 0, 0
	record := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case err == nil:
			ok++
		case isCapError(err):
			capRejected++
		default:
			otherErr++
		}
	}
	wg.Add(2)
	go func() { defer wg.Done(); record(mustEnsure(t, store, tn1, "via-deploy", owner)) }()
	go func() {
		defer wg.Done()
		_, err := store.CreateProject(asTenant(tn2), ports.CreateProjectInput{TenantID: tn2, Name: "via-api", CreatedBy: owner})
		record(err)
	}()
	wg.Wait()

	if ok != 1 || capRejected != 1 || otherErr != 0 {
		t.Fatalf("deploy + API must share the cap: exactly one wins; ok=%d capRejected=%d otherErr=%d (want 1,1,0)", ok, capRejected, otherErr)
	}
}

// Scenario 5 — concurrency: 8 simultaneous NEW-project deploys for one
// at-boundary account; the advisory-lock atomic guard means exactly one wins,
// the rest are cap-rejected, and the final count never exceeds the cap.
func TestProjectCapOnDeploy_ConcurrentDeploysNeverExceedCap(t *testing.T) {
	store, std := projectCapStore(t)
	const owner, acct, tn1, tn2 = "m5-cc-uid", "acct-m5cc", "tn-m5cc-a", "tn-m5cc-b"
	projectCapSeed(t, std, owner, acct, tn1, tn2, "pro") // pro cap = 10
	t.Cleanup(func() { projectCapCleanup(t, std, owner) })

	const capLimit = 10
	for i := 0; i < capLimit-1; i++ { // pre-fill to cap-1 → exactly one slot left
		if err := mustEnsure(t, store, tn1, "pre-"+string(rune('a'+i)), owner); err != nil {
			t.Fatalf("pre-fill %d: %v", i, err)
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, capRejected, otherErr := 0, 0, 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tn := tn1
			if i%2 == 0 {
				tn = tn2 // half via the other tenant of the same account
			}
			err := mustEnsure(t, store, tn, "race-"+string(rune('a'+i)), owner)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case isCapError(err):
				capRejected++
			default:
				otherErr++
			}
		}(i)
	}
	wg.Wait()

	if ok != 1 {
		t.Fatalf("exactly ONE deploy should win the last slot; %d succeeded", ok)
	}
	if otherErr != 0 || capRejected != 7 {
		t.Fatalf("losers must all be PROJECTS_EXCEED_PLAN (not lock/tx errors); capRejected=%d otherErr=%d want 7,0", capRejected, otherErr)
	}
	var n int
	if err := std.QueryRow(`SELECT count(*) FROM projects WHERE tenant_id IN (SELECT id FROM tenants WHERE account_id=$1)`, acct).Scan(&n); err != nil {
		t.Fatalf("final count: %v", err)
	}
	if n != capLimit {
		t.Fatalf("final count = %d, must equal cap %d (atomic guard held)", n, capLimit)
	}
}

// Scenario 6 — the DEPLOY path with the env-lock ALREADY held on the caller's tx
// (the real build-submit shape): EnsureProject takes the account-lock on the SAME
// tx (env→account order, no nested lock) and still enforces the cap — no deadlock.
func TestProjectCapOnDeploy_DeployWithHeldEnvLock_EnforcesCapNoDeadlock(t *testing.T) {
	store, std := projectCapStore(t)
	pool := scopedRequestPool(t).Pool
	const owner, acct, tn1, tn2 = "m5-el-uid", "acct-m5el", "tn-m5el-a", "tn-m5el-b"
	projectCapSeed(t, std, owner, acct, tn1, tn2, "starter") // cap = 1
	t.Cleanup(func() { projectCapCleanup(t, std, owner) })

	// Fill the single slot so the next NEW deploy is over cap.
	if err := mustEnsure(t, store, tn1, "only", owner); err != nil {
		t.Fatalf("seed first project: %v", err)
	}

	ctx := asTenant(tn2)
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Hold the environment advisory lock first (what build-submit does).
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", tn2+":newdeploy:production"); err != nil {
		t.Fatalf("acquire env-lock: %v", err)
	}
	// Now the deploy auto-create on the SAME tx — account-lock taken after env-lock.
	err = store.EnsureProject(ctx, &db.PgxTxAdapter{Tx: tx}, tn2, "newdeploy", owner)
	if !isCapError(err) {
		t.Fatalf("deploy on a held-env-lock tx over cap must be PROJECTS_EXCEED_PLAN (and not deadlock); got %v", err)
	}
}

// Scenario 7 — the RLS-context guard. The real build-submit tx sets
// `app.tenant_id = <deploying tenant>` (the projects RLS policy). The per-Account
// cap count is a CROSS-TENANT aggregate that MUST NOT be filtered by that policy,
// otherwise it would degrade to per-tenant. This reproduces that exact context
// (set app.tenant_id to the deploying tenant, then drive the deploy on the same
// tx) and asserts the cap still binds across tenants — pinning the documented
// BYPASSRLS/owner invariant for the count (see enforceProjectCapTx).
func TestProjectCapOnDeploy_CrossTenantCountUnaffectedByRLSContext(t *testing.T) {
	store, std := projectCapStore(t)
	pool := scopedRequestPool(t).Pool
	const owner, acct, tn1, tn2 = "m5-rls-uid", "acct-m5rls", "tn-m5rls-a", "tn-m5rls-b"
	projectCapSeed(t, std, owner, acct, tn1, tn2, "starter") // cap = 1
	t.Cleanup(func() { projectCapCleanup(t, std, owner) })

	// Fill the single slot under tn1, so the account is AT cap via a DIFFERENT
	// tenant than the one that will deploy.
	if _, err := std.Exec(`INSERT INTO projects (id,tenant_id,name,created_by) VALUES ($1,$2,$3,$4)`, "proj-rls", tn1, "existing", owner); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ctx := asTenant(tn2)
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Reproduce the build-submit RLS context: app.tenant_id = the DEPLOYING tenant (tn2).
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tn2); err != nil {
		t.Fatalf("set app.tenant_id: %v", err)
	}
	// Deploy a NEW project under tn2. The cross-tenant count must still see tn1's
	// project (count=1 >= cap=1) and block — if the count were RLS-filtered to
	// app.tenant_id=tn2 it would see 0 and wrongly allow.
	err = store.EnsureProject(ctx, &db.PgxTxAdapter{Tx: tx}, tn2, "rls-newproj", owner)
	if !isCapError(err) {
		t.Fatalf("cross-tenant cap must bind despite app.tenant_id being set to the deploying tenant; got %v", err)
	}
}
