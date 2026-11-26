// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/lock"
)

// loop_runtime_worker is the one role that may cross tenants, and migration 000023
// granted it only the tables that existed then. On a database built from the
// migrations it held nothing on `accounts` or `deploy_events`, so the two
// cross-tenant writes these tests run were refused with `permission denied`
// (issue #385). Migration 000068 grants exactly what they execute.
//
// They exercise the privileges themselves, so they only mean something on a
// database built from the migrations (not schema.gen.sql with blanket grants)
// and with LOOP_RUNTIME_WORKER_DATABASE_URL connecting as loop_runtime_worker:
//
//	DATABASE_URL=<loop_runtime_api> LOOP_RUNTIME_WORKER_DATABASE_URL=<loop_runtime_worker> \
//	LOOP_RUNTIME_OWNER_DATABASE_URL=<loop-runtime> go test ./tests/integration/ -run WorkerRole -v

// requireTheWorkerRolesOwnPrivileges skips unless the worker DSN connects as a
// role whose table privileges are what the migrations granted: a superuser, a
// BYPASSRLS role or the tables' owner passes every check below whatever the
// grants are.
func requireTheWorkerRolesOwnPrivileges(t *testing.T) {
	t.Helper()
	_ = testPool(t) // skips when PostgreSQL is unreachable
	pool := workerPool(t)
	var unconstrained bool
	if err := pool.QueryRow(context.Background(),
		`SELECT r.rolsuper OR r.rolbypassrls OR EXISTS (
		        SELECT 1 FROM pg_class c WHERE c.relname = 'accounts' AND c.relowner = r.oid)
		   FROM pg_roles r WHERE r.rolname = current_user`).Scan(&unconstrained); err != nil {
		t.Fatalf("read the worker role's posture: %v", err)
	}
	if unconstrained {
		// Such a role cannot reproduce https://github.com/getloopscale/loop-runtime/issues/385.
		t.Skip("LOOP_RUNTIME_WORKER_DATABASE_URL is not the constrained cross-tenant role; point it at loop_runtime_worker")
	}
}

// POST /internal/bootstrap runs on the cross-tenant pool and creates the
// identity's Account before its tenant: it needs SELECT and INSERT on
// `accounts`.
func TestWorkerRoleProvisionsTheAccountOnTheBootstrapPath(t *testing.T) {
	requireTheWorkerRolesOwnPrivileges(t)
	seed := openSeed(t)
	const userID = "11111111-2222-3333-4444-999999999385"
	bootstrapCleanup(t, seed, userID)
	t.Cleanup(func() { bootstrapCleanup(t, seed, userID) })

	rec, resp := postBootstrap(t, bootstrapHandler(t), map[string]string{
		"admin_user_id": userID, "admin_login": "grants-boot", "admin_email": "grants-boot@e2e.test",
		"tenant_slug": "grants-boot",
	})

	if rec.Code != http.StatusOK || resp.TenantID == "" {
		t.Fatalf("bootstrap on the worker role = %d %s", rec.Code, rec.Body.String())
	}
}

// Release activation (convergence loop) and the reconcile / reaper
// deploy.failed emit stage the outbox row inside the environment lock's
// transaction on the background pool: INSERT on `deploy_events` plus the
// BIGSERIAL's sequence.
func TestWorkerRoleWritesTheDeployOutboxInsideTheEnvironmentLock(t *testing.T) {
	requireTheWorkerRolesOwnPrivileges(t)
	seed := openSeed(t)
	const owner, acct, tn1, tn2 = "rls-outbox-uid", "acct-rls-outbox", "tn-rls-outbox-a", "tn-rls-outbox-b"
	projectCapSeed(t, seed, owner, acct, tn1, tn2, "starter")
	t.Cleanup(func() { projectCapCleanup(t, seed, owner) }) // deploy_events cascades with the tenant
	ctx := context.Background()

	_, err := lock.WithEnvironmentLock(ctx, workerPool(t), tn1, "outbox-project", "production",
		func(tx lock.Tx) (struct{}, error) {
			return struct{}{}, store.NewDeployEventStoreSqlc().AppendDeployEvent(ctx, &db.PgxTxAdapter{Tx: tx}, store.DeployEvent{
				TransitionID: "tr-rls-outbox-385", EventType: "deploy.succeeded", TenantID: tn1,
				ProjectID: "outbox-project", DeployID: "rel-rls-outbox", Payload: map[string]any{"status": "live"},
			})
		})

	if err != nil {
		t.Fatalf("deploy outbox write on the worker role: %v", err)
	}
	if got := deployEventsOf(t, tn1); got != 1 {
		t.Fatalf("deploy events recorded for %s = %d; want 1", tn1, got)
	}
}

// deployEventsOf reads the outbox through the request role scoped to the
// tenant: loop_runtime_worker only writes it, and the drain belongs to the control
// plane.
func deployEventsOf(t *testing.T, tenant string) int {
	t.Helper()
	ctx := context.Background()
	tx, err := poolFor(t, dbURL()).BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenant); err != nil {
		t.Fatalf("scope to %s: %v", tenant, err)
	}
	var n int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM deploy_events WHERE tenant_id = $1", tenant).Scan(&n); err != nil {
		t.Fatalf("count deploy events: %v", err)
	}
	return n
}
