// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loopzero/api/internal/db"
)

// Real-Postgres tests for the deploy_events transactional outbox. Skipped when
// no DB is reachable (hermetic tier); RUN by provisioning an ephemeral PG +
// migrating + exporting DATABASE_URL (the operator/CI harness).
//
// These prove the property the whole outbox design rests on: the INSERT
// commits/rolls back WITH the caller's transaction (transactional outbox,
// Option C, MEET-2026-07-03-005). A fake store cannot prove this — only a real
// tx that actually rolls back can.

func deployEventPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, fixtureDBURL())
	if err != nil {
		t.Skipf("deploy_events PG test skipped: cannot create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("deploy_events PG test skipped: cannot ping PG: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// seedDeployEventTenant bootstraps the FK graph the deploy_events.tenant_id →
// tenants(id) FK requires. The graph is cyclic (accounts.owner→users,
// tenants.owner→users, tenants.account→accounts, users.active_tenant→tenants);
// it's broken by inserting the user with a NULL active_tenant_id first. Cleanup
// runs in reverse (tenants CASCADE-drops the deploy_events rows).
func seedDeployEventTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) {
	t.Helper()
	userID := tenantID + "-owner"
	acctID := tenantID + "-acct"

	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, display_name, username, active_tenant_id)
		 VALUES ($1, $2, 'DE Owner', $3, NULL) ON CONFLICT (id) DO NOTHING`,
		userID, userID+"@example.test", userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO accounts (id, display_name, owner_id)
		 VALUES ($1, 'DE Account', $2) ON CONFLICT (id) DO NOTHING`,
		acctID, userID); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, slug, owner_id, account_id)
		 VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`,
		tenantID, tenantID+"-slug", userID, acctID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM tenants WHERE id=$1`, tenantID) // CASCADE deploy_events
		_, _ = pool.Exec(bg, `DELETE FROM accounts WHERE id=$1`, acctID)
		_, _ = pool.Exec(bg, `DELETE FROM users WHERE id=$1`, userID)
	})
}

func TestDeployEventStore_PG_CommitPersistsRow(t *testing.T) {
	pool := deployEventPool(t)
	ctx := context.Background()
	const tenantID = "tn-de-commit"
	seedDeployEventTenant(t, ctx, pool, tenantID)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	s := NewDeployEventStoreSqlc()
	ev := DeployEvent{
		TransitionID: "tr-commit-1", EventType: "deploy.succeeded", TenantID: tenantID,
		ProjectID: "my-proj", DeployID: "rel-abc", CorrelationID: "corr-1",
		Payload: map[string]any{"event": "deploy.succeeded", "project": "my-proj", "status": "success"},
	}
	if err := s.AppendDeployEvent(ctx, &db.PgxTxAdapter{Tx: tx}, ev); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("AppendDeployEvent on tx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var (
		gotType, gotProject, gotDeploy, gotCorr string
		payload                                 []byte
	)
	err = pool.QueryRow(ctx,
		`SELECT event_type, project_id, deploy_id, correlation_id, payload
		 FROM deploy_events WHERE transition_id=$1`, "tr-commit-1").
		Scan(&gotType, &gotProject, &gotDeploy, &gotCorr, &payload)
	if err != nil {
		t.Fatalf("read back committed row: %v", err)
	}
	if gotType != "deploy.succeeded" || gotProject != "my-proj" || gotDeploy != "rel-abc" || gotCorr != "corr-1" {
		t.Errorf("row = (%s,%s,%s,%s); want (deploy.succeeded,my-proj,rel-abc,corr-1)", gotType, gotProject, gotDeploy, gotCorr)
	}
	if string(payload) == "" || string(payload)[0] != '{' {
		t.Errorf("payload not a JSON object: %q", string(payload))
	}
}

// The transactional-outbox guarantee: an INSERT on a tx that ROLLS BACK leaves
// zero rows. This is why the kernel writes the event on the activation tx —
// a failed deploy activation must NOT emit a phantom deploy.succeeded.
func TestDeployEventStore_PG_RollbackLeavesNoRow(t *testing.T) {
	pool := deployEventPool(t)
	ctx := context.Background()
	const tenantID = "tn-de-rollback"
	seedDeployEventTenant(t, ctx, pool, tenantID)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	s := NewDeployEventStoreSqlc()
	ev := DeployEvent{
		TransitionID: "tr-rollback-1", EventType: "deploy.succeeded", TenantID: tenantID,
		ProjectID: "my-proj", DeployID: "rel-xyz",
		Payload: map[string]any{"status": "success"},
	}
	if err := s.AppendDeployEvent(ctx, &db.PgxTxAdapter{Tx: tx}, ev); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("AppendDeployEvent on tx: %v", err)
	}
	// Roll back — the deploy "failed" after the event was staged.
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM deploy_events WHERE transition_id=$1`, "tr-rollback-1").Scan(&count); err != nil {
		t.Fatalf("count after rollback: %v", err)
	}
	if count != 0 {
		t.Errorf("deploy_events rows after rollback = %d, want 0 (the outbox INSERT MUST roll back with the transaction)", count)
	}
}

// UNIQUE(transition_id) rejects a double emit for the same activation entry.
func TestDeployEventStore_PG_DuplicateTransitionRejected(t *testing.T) {
	pool := deployEventPool(t)
	ctx := context.Background()
	const tenantID = "tn-de-dup"
	seedDeployEventTenant(t, ctx, pool, tenantID)
	s := NewDeployEventStoreSqlc()
	ev := DeployEvent{
		TransitionID: "tr-dup-1", EventType: "deploy.succeeded", TenantID: tenantID,
		ProjectID: "my-proj", DeployID: "rel-1", Payload: map[string]any{"status": "success"},
	}

	tx1, _ := pool.Begin(ctx)
	if err := s.AppendDeployEvent(ctx, &db.PgxTxAdapter{Tx: tx1}, ev); err != nil {
		_ = tx1.Rollback(ctx)
		t.Fatalf("first insert: %v", err)
	}
	if err := tx1.Commit(ctx); err != nil {
		t.Fatalf("commit first: %v", err)
	}

	tx2, _ := pool.Begin(ctx)
	err := s.AppendDeployEvent(ctx, &db.PgxTxAdapter{Tx: tx2}, ev) // same transition_id
	_ = tx2.Rollback(ctx)
	if err == nil {
		t.Error("duplicate transition_id was accepted; want UNIQUE violation (dedup guard)")
	}
}
