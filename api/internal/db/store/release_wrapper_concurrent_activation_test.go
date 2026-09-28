// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// release_wrapper_concurrent_activation_test.go — T5.1 CV-M-1 of plan
// 2026-06-01-p3-infra-observability-test-debt-plan.md v1.1.
//
// Tests the wiring between the partial unique index
// `idx_releases_active_unique` (migration 000022) and the typed
// `CONCURRENT_ACTIVATION` error returned by ReleaseStoreSqlc.UpdateStatus.
//
// Two layers:
//   1. Pure-Go: isReleasesActiveUniqueViolation classifies pgconn.PgError
//      by Code + ConstraintName (no PG required).
//   2. PG-backed: end-to-end test that creates a release, activates it,
//      then attempts a second activation for the same env; asserts the
//      returned error is `CONCURRENT_ACTIVATION`.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loopzero/api/internal/db"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// TestIsReleasesActiveUniqueViolation_OnlyIdxReleasesActiveUnique:
// classifier matches ONLY the named constraint, not generic 23505
// violations (e.g., builds_pkey).
func TestIsReleasesActiveUniqueViolation_MatchesOnlyTargetIndex(t *testing.T) {
	matching := &pgconn.PgError{Code: "23505", ConstraintName: "idx_releases_active_unique"}
	if !isReleasesActiveUniqueViolation(matching) {
		t.Error("MUST classify Code=23505 + ConstraintName=idx_releases_active_unique")
	}

	wrongConstraint := &pgconn.PgError{Code: "23505", ConstraintName: "builds_pkey"}
	if isReleasesActiveUniqueViolation(wrongConstraint) {
		t.Error("MUST NOT match other 23505 unique-violations (e.g., builds_pkey)")
	}

	wrongCode := &pgconn.PgError{Code: "23503", ConstraintName: "idx_releases_active_unique"}
	if isReleasesActiveUniqueViolation(wrongCode) {
		t.Error("MUST NOT match non-23505 codes (e.g., 23503 fk violation)")
	}

	notPgError := fmt.Errorf("generic wrap: %w", errors.New("connection refused"))
	if isReleasesActiveUniqueViolation(notPgError) {
		t.Error("MUST NOT match non-pg errors")
	}

	if isReleasesActiveUniqueViolation(nil) {
		t.Error("MUST NOT match nil")
	}
}

// TestIsReleasesActiveUniqueViolation_UnwrapsChain: classifier
// uses errors.As so it walks the wrap chain (sqlc wraps in fmt.Errorf
// with %w; legacy paths wrap multiple times before reaching the route).
func TestIsReleasesActiveUniqueViolation_UnwrapsChain(t *testing.T) {
	inner := &pgconn.PgError{Code: "23505", ConstraintName: "idx_releases_active_unique"}
	wrapped := fmt.Errorf("sqlc: update release status: %w", inner)
	doubleWrapped := fmt.Errorf("release_store: state machine: %w", wrapped)

	if !isReleasesActiveUniqueViolation(wrapped) {
		t.Error("MUST unwrap one level of fmt.Errorf %w")
	}
	if !isReleasesActiveUniqueViolation(doubleWrapped) {
		t.Error("MUST unwrap multiple levels of wrapping")
	}
}

func concurrentActivationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, fixtureDBURL())
	if err != nil {
		t.Skipf("CV-M-1 test skipped: cannot create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("CV-M-1 test skipped: cannot ping PG: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// requireActiveUniqueIndexAndReleaseStore skips when migration 000022
// hasn't been applied OR when the releases table doesn't have the
// expected schema (state column, etc.).
func requireActiveUniqueIndexAndReleaseStore(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE schemaname = 'public'
			  AND indexname = 'idx_releases_active_unique'
		)`).Scan(&exists)
	if err != nil {
		t.Skipf("CV-M-1 test skipped: cannot query pg_indexes: %v", err)
	}
	if !exists {
		t.Skipf("CV-M-1 test skipped: idx_releases_active_unique missing (run migration 000022)")
	}
}

// TestUpdateStatus_ConcurrentActivation_ReturnsTypedError: end-to-end
// proof that the wire from PG SQLSTATE 23505 → CONCURRENT_ACTIVATION
// works. Setup: seed release R1 already ACTIVE for (tn, proj, env).
// Action: attempt to UPDATE release R2 (PENDING) to ACTIVE for the
// SAME (tn, proj, env). The partial unique index rejects; wrapper
// detects + returns NewConcurrentActivation.
//
// M9 5b (ADR 2026-07-03-m9-5b-nil-tx-status-atomicity, pre-condition #1):
// this test now calls the REAL ReleaseStoreSqlc.UpdateStatus end-to-end
// (previously it did a raw pool.Exec UPDATE + SYNTHESIZED the typed error,
// so the 23505→CONCURRENT_ACTIVATION→409 wire could be severed and the test
// would stay green). The real call exercises the full chain: GetByIDTx →
// SafeTransition(PENDING, SyncCompleted)=ACTIVE → UpdateReleaseStatus →
// partial-unique-index 23505 → isReleasesActiveUniqueViolation →
// NewConcurrentActivation.
//
// Operator HARD STOP: the behavioral assertion needs live PG with migration
// 000022 (no hermetic PG tier). concurrentActivationPool honest-skips at
// pool-create BEFORE the store call, so the real-call path (constructing the
// store, calling UpdateStatus, the typed assertion) is compiled + link-checked
// every hermetic run while the 409 wire is proven on the operator live-PG tier
// (per .claude/rules/cleanroom-up-live-test-mandate.md).
func TestUpdateStatus_ConcurrentActivation_ReturnsTypedError(t *testing.T) {
	pool := concurrentActivationPool(t)
	requireActiveUniqueIndexAndReleaseStore(t, pool)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantID := "tn-cv-m-1-" + suffix
	projectID := "proj-cv-m-1"
	env := "production"

	buildID1 := "bld-cv-m-1-a-" + suffix
	releaseID1 := "rel-cv-m-1-a-" + suffix
	buildID2 := "bld-cv-m-1-b-" + suffix
	releaseID2 := "rel-cv-m-1-b-" + suffix

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			"DELETE FROM releases WHERE tenant_id = $1", tenantID)
		_, _ = pool.Exec(context.Background(),
			"DELETE FROM builds WHERE tenant_id = $1", tenantID)
	})

	// The migration 000061 trigger resolves builds.cell_id from the tenant and
	// fails loudly if it does not exist; this fixture predates the trigger.
	seedTenantRowForStore(t, pool, tenantID)

	// Seed two builds + R1 ACTIVE + R2 PENDING for same env.
	for _, p := range []struct {
		bid, rid, status string
	}{
		{buildID1, releaseID1, "ACTIVE"},
		{buildID2, releaseID2, "PENDING"},
	} {
		_, err := pool.Exec(ctx,
			`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
			 VALUES ($1, $2, $3, $4, 'SUCCEEDED', $5, 'system:test')`,
			p.bid, tenantID, projectID, env, "corr-"+p.rid)
		if err != nil {
			t.Fatalf("seed build %s: %v", p.rid, err)
		}
		_, err = pool.Exec(ctx,
			`INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, 'system:test')`,
			p.rid, p.bid, tenantID, projectID, env, p.status, "corr-"+p.rid)
		if err != nil {
			t.Fatalf("seed release %s status=%q: %v", p.rid, p.status, err)
		}
	}

	// Call the REAL store end-to-end. PgxAdapter is the SqlcBridge gen() needs;
	// the adapter is passed as a non-nil tx (satisfies the M9 5b reject-nil
	// guard — this IS the withEnvironmentLock-scoped path in production).
	// SyncCompleted is the real PENDING→ACTIVE event, so SafeTransition passes
	// and UpdateReleaseStatus fires into the partial unique index.
	adapter := &db.PgxAdapter{Pool: pool}
	store := NewReleaseStoreSqlc(adapter, NoopEventStore{})

	_, err := store.UpdateStatus(ctx, adapter, releaseID2, string(statemachine.ReleaseSyncCompleted))
	if err == nil {
		t.Fatal("expected CONCURRENT_ACTIVATION from the partial-unique-index, got nil")
	}
	// Assert the REAL typed wire (not a synthesized value): the store must
	// return a *LoopZeroError with Code CONCURRENT_ACTIVATION.
	var te *loopzeroerrors.LoopZeroError
	if !errors.As(err, &te) || te.Code != "CONCURRENT_ACTIVATION" {
		t.Errorf("real UpdateStatus must return typed CONCURRENT_ACTIVATION; got %v", err)
	}
}

// seedTenantRowForStore creates the user/account/tenant a `builds` row needs.
// See the note at the call site: migration 000061's trigger resolves
// `builds.cell_id` from the tenant and refuses when it is absent.
func seedTenantRowForStore(t *testing.T, pool *pgxpool.Pool, tenantID string) {
	t.Helper()

	ctx := context.Background()
	owner := "u-" + tenantID
	account := "acct-" + tenantID

	for _, step := range []struct {
		what string
		sql  string
		args []any
	}{
		{"user", `INSERT INTO users (id,email,display_name,username)
		          VALUES ($1,$1||'@fixture.test','F',$1) ON CONFLICT (id) DO NOTHING`, []any{owner}},
		{"account", `INSERT INTO accounts (id,display_name,plan,owner_id)
		             VALUES ($1,'F','starter',$2) ON CONFLICT (id) DO NOTHING`, []any{account, owner}},
		{"tenant", `INSERT INTO tenants (id,display_name,slug,owner_id,account_id,cell_id)
		            VALUES ($1,$1,$1,$2,$3,'cell-0') ON CONFLICT (id) DO NOTHING`, []any{tenantID, owner, account}},
	} {
		if _, err := pool.Exec(ctx, step.sql, step.args...); err != nil {
			t.Fatalf("seed %s for %s: %v", step.what, tenantID, err)
		}
	}
}
