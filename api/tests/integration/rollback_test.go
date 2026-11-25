// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package integration tests rollback behavior with real PostgreSQL.
//
// These tests prove:
// 1. Rollback supersedes the current ACTIVE release and reactivates the previous one
// 2. Concurrent rollbacks are serialized — exactly one succeeds
//
// They run under the role split production uses: fixtures are written through
// the cross-tenant fixture pool (seedDBURL), and the orchestrator runs on the
// tenant-scoped request pool (tenantScopedRequestPool) with the tenant in its
// context, the way the rollback handler reaches it. Under fail-closed Row Level Security a
// store read on an unscoped connection sees no release at all, which is how
// these tests used to report NO_PREVIOUS_RELEASE (issue #387).
//
// These tests require a real, already-migrated PostgreSQL instance. They skip
// gracefully if DATABASE_URL is not set and the default connection fails.
package integration

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/lock"
	"github.com/getloopscale/loopzero/api/internal/release"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/dbtx"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// rollbackTenant is the tenant every rollback fixture belongs to.
const rollbackTenant = "tenant-test"

// --- Fake external services for rollback tests ---

// fakeRollbackDelivery stands in for the container delivery activator: these
// tests prove the orchestrator's locking and state transitions against real
// PostgreSQL, not the GitOps push or the sync, which have their own tests.
type fakeRollbackDelivery struct{}

func (fakeRollbackDelivery) Activate(context.Context, deploycontracts.ActivationBuild, deploycontracts.BuildCallbackRequest) error {
	return nil
}

func (fakeRollbackDelivery) Rollback(context.Context, deploycontracts.RollbackDeliveryRequest) error {
	return nil
}

func (fakeRollbackDelivery) AwaitRollback(context.Context, deploycontracts.RollbackDeliveryRequest) error {
	return nil
}

func (fakeRollbackDelivery) RollbackConverged(context.Context, deploycontracts.RollbackDeliveryRequest) (bool, error) {
	return true, nil
}

// --- Rollback-specific test data ---

// seedRollbackData creates the baseline data for rollback tests: tenant, user, project,
// a build record (required by FK), and two releases with an environment_target.
// R1 is ACTIVE, R2 is SUPERSEDED (the previous release to rollback to).
func seedRollbackData(t *testing.T, pool db.DBTX, testID string) (r1ID, r2ID, buildID string) {
	t.Helper()
	ctx := context.Background()

	tenantID := "tenant-test"
	projectID := "proj-test1234"
	env := "production"
	buildID = fmt.Sprintf("bld-rollback-%s", testID)
	r1ID = fmt.Sprintf("rel-active-%s", testID)
	r2ID = fmt.Sprintf("rel-superseded-%s", testID)

	// Insert a build (FK dependency for releases)
	_, err := pool.Exec(ctx,
		`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (id) DO NOTHING`,
		buildID, tenantID, projectID, env, string(statemachine.BPhSucceeded), "corr-rollback", "user-test",
	)
	if err != nil {
		t.Fatalf("failed to seed build: %v", err)
	}

	// Insert R2 first (older, SUPERSEDED) — created_at will be earlier
	_, err = pool.Exec(ctx,
		`INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by, git_commit, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW() - INTERVAL '10 minutes')
		 ON CONFLICT (id) DO NOTHING`,
		r2ID, buildID, tenantID, projectID, env, string(statemachine.RPhSuperseded), "corr-rollback", "user-test", "commit-r2",
	)
	if err != nil {
		t.Fatalf("failed to seed release R2 (SUPERSEDED): %v", err)
	}

	// Insert R1 (newer, ACTIVE)
	_, err = pool.Exec(ctx,
		`INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by, git_commit, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		 ON CONFLICT (id) DO NOTHING`,
		r1ID, buildID, tenantID, projectID, env, string(statemachine.RPhActive), "corr-rollback", "user-test", "commit-r1",
	)
	if err != nil {
		t.Fatalf("failed to seed release R1 (ACTIVE): %v", err)
	}

	// Set environment_target pointing to R1 as active
	_, err = pool.Exec(ctx,
		`INSERT INTO environment_targets (tenant_id, project_id, environment, desired_release_id, active_release_id, updated_by, correlation_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (tenant_id, project_id, environment)
		 DO UPDATE SET desired_release_id = $4, active_release_id = $5, updated_by = $6, correlation_id = $7`,
		tenantID, projectID, env, r1ID, r1ID, "user-test", "corr-rollback",
	)
	if err != nil {
		t.Fatalf("failed to seed environment_target: %v", err)
	}

	return r1ID, r2ID, buildID
}

// cleanupRollbackData removes rollback test records.
func cleanupRollbackData(t *testing.T, pool db.DBTX, testID string) {
	t.Helper()
	ctx := context.Background()

	buildID := fmt.Sprintf("bld-rollback-%s", testID)
	r1ID := fmt.Sprintf("rel-active-%s", testID)
	r2ID := fmt.Sprintf("rel-superseded-%s", testID)
	r3ID := fmt.Sprintf("rel-superseded2-%s", testID)

	// Clean in reverse dependency order
	for _, q := range []struct {
		sql  string
		args []interface{}
	}{
		{"DELETE FROM environment_targets WHERE tenant_id = 'tenant-test' AND project_id = 'proj-test1234' AND environment = 'production'", nil},
		{"DELETE FROM releases WHERE id IN ($1, $2, $3)", []interface{}{r1ID, r2ID, r3ID}},
		{"DELETE FROM builds WHERE id = $1", []interface{}{buildID}},
	} {
		if _, err := pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Errorf("cleanup %q: %v", q.sql, err)
		}
	}
}

func TestRollback_SupersedesPreviousActiveRelease(t *testing.T) {
	// Arrange: R1 is ACTIVE, R2 is SUPERSEDED.
	// Act: rollback (no explicit target — uses most recent SUPERSEDED).
	// Assert: R1 → ROLLED_BACK, R2 → ACTIVE, environment_target → R2.

	seed := seedPool(t)
	seedTestData(t, seed)
	fixtures := &db.PgxAdapter{Pool: seed}

	testID := fmt.Sprintf("supersede-%d", time.Now().UnixNano())
	cleanupRollbackData(t, fixtures, testID)
	t.Cleanup(func() { cleanupRollbackData(t, fixtures, testID) })

	r1ID, r2ID, _ := seedRollbackData(t, fixtures, testID)

	// Create real stores on the request pool, as the server wires them.
	adapter := tenantScopedRequestPool(t, 4)
	releaseStore := store.NewReleaseStoreSqlc(adapter, store.NoopEventStore{}) // T1.1c2c (plan 2026-05-29-sqlc-migration v1.3)
	envTargetStore := store.NewPostgresEnvironmentTargetStore(adapter, adapter)
	locker := &lock.PoolEnvironmentLocker{Pool: adapter.Pool}

	// Create rollback orchestrator with fake external services
	orch := release.NewRollbackOrchestrator(release.RollbackOrchestratorDeps{
		Releases:   releaseStore,
		EnvTargets: envTargetStore,
		Locker:     locker,
		Delivery:   fakeRollbackDelivery{},
		Logger:     slog.Default(),
	})

	// Act — scoped to the caller's tenant, as scopeDatabaseToCaller scopes a request.
	ctx := dbtx.WithTenantScope(context.Background(), rollbackTenant)
	result, err := orch.Rollback(ctx, release.RollbackRequest{
		TenantID:      "tenant-test",
		ProjectID:     "proj-test1234",
		ProjectName:   "test-project",
		Environment:   "production",
		CorrelationID: "corr-rollback-test",
		UserID:        "user-test",
	})
	orch.Wait() // the release state settles after the answer

	// Assert: no error
	if err != nil {
		t.Fatalf("rollback failed: %v", err)
	}

	// Assert: result contains correct releases
	if result.RolledBackRelease.ID != r1ID {
		t.Fatalf("expected rolled back release ID %s, got %s", r1ID, result.RolledBackRelease.ID)
	}
	if result.ReactivatedRelease.ID != r2ID {
		t.Fatalf("expected reactivated release ID %s, got %s", r2ID, result.ReactivatedRelease.ID)
	}

	// Assert: R1 status is ROLLED_BACK
	r1, err := releaseStore.GetByID(ctx, r1ID)
	if err != nil {
		t.Fatalf("failed to fetch R1: %v", err)
	}
	if r1.Status != string(statemachine.RPhRolledBack) {
		t.Fatalf("expected R1 status %s, got %s", statemachine.RPhRolledBack, r1.Status)
	}

	// Assert: R2 status is ACTIVE
	r2, err := releaseStore.GetByID(ctx, r2ID)
	if err != nil {
		t.Fatalf("failed to fetch R2: %v", err)
	}
	if r2.Status != string(statemachine.RPhActive) {
		t.Fatalf("expected R2 status %s, got %s", statemachine.RPhActive, r2.Status)
	}

	// Assert: environment_target active_release_id points to R2
	target, err := envTargetStore.Get(ctx, "tenant-test", "proj-test1234", "production")
	if err != nil {
		t.Fatalf("failed to fetch environment_target: %v", err)
	}
	if target == nil {
		t.Fatal("environment_target not found")
	}
	if target.ActiveReleaseID == nil || *target.ActiveReleaseID != r2ID {
		activeID := "<nil>"
		if target.ActiveReleaseID != nil {
			activeID = *target.ActiveReleaseID
		}
		t.Fatalf("expected environment_target active_release_id %s, got %s", r2ID, activeID)
	}
	if target.DesiredReleaseID != r2ID {
		t.Fatalf("expected environment_target desired_release_id %s, got %s", r2ID, target.DesiredReleaseID)
	}

	t.Logf("rollback succeeded: R1(%s) → ROLLED_BACK, R2(%s) → ACTIVE, env_target → R2", r1ID, r2ID)
}

func TestConcurrentRollbacks_ExactlyOneSucceeds(t *testing.T) {
	// Arrange: R1 is ACTIVE, R2 and R3 are SUPERSEDED.
	// Act: launch 2 goroutines both attempting rollback simultaneously.
	// Assert: exactly one succeeds, final state is consistent (exactly 1 ACTIVE release).

	seed := seedPool(t)
	seedTestData(t, seed)
	fixtures := &db.PgxAdapter{Pool: seed}

	testID := fmt.Sprintf("concurrent-%d", time.Now().UnixNano())
	cleanupRollbackData(t, fixtures, testID)
	t.Cleanup(func() { cleanupRollbackData(t, fixtures, testID) })

	r1ID, r2ID, buildID := seedRollbackData(t, fixtures, testID)

	// Insert a third release (R3, also SUPERSEDED, older than R2)
	r3ID := fmt.Sprintf("rel-superseded2-%s", testID)
	_, err := seed.Exec(context.Background(),
		`INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by, git_commit, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW() - INTERVAL '20 minutes')
		 ON CONFLICT (id) DO NOTHING`,
		r3ID, buildID, "tenant-test", "proj-test1234", "production",
		string(statemachine.RPhSuperseded), "corr-rollback", "user-test", "commit-r3",
	)
	if err != nil {
		t.Fatalf("failed to seed release R3: %v", err)
	}

	// Create shared stores on the request pool, as the server wires them. Two
	// connections at least, so both rollbacks reach the environment lock.
	adapter := tenantScopedRequestPool(t, 4)
	releaseStore := store.NewReleaseStoreSqlc(adapter, store.NoopEventStore{}) // T1.1c2c (plan 2026-05-29-sqlc-migration v1.3)
	envTargetStore := store.NewPostgresEnvironmentTargetStore(adapter, adapter)
	locker := &lock.PoolEnvironmentLocker{Pool: adapter.Pool}
	ctx := dbtx.WithTenantScope(context.Background(), rollbackTenant)

	// Launch concurrent rollbacks
	type rollbackResult struct {
		result *release.RollbackResult
		err    error
	}

	results := make([]rollbackResult, 2)
	ready := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	for i := 0; i < 2; i++ {
		go func(idx int) {
			defer wg.Done()

			// Each goroutine gets its own orchestrator (stateless, just holds deps)
			orch := release.NewRollbackOrchestrator(release.RollbackOrchestratorDeps{
				Releases:   releaseStore,
				EnvTargets: envTargetStore,
				Locker:     locker,
				Delivery:   fakeRollbackDelivery{},
				Logger:     slog.Default(),
			})

			<-ready // Wait for barrier

			res, err := orch.Rollback(ctx, release.RollbackRequest{
				TenantID:      "tenant-test",
				ProjectID:     "proj-test1234",
				ProjectName:   "test-project",
				Environment:   "production",
				CorrelationID: fmt.Sprintf("corr-concurrent-%d", idx),
				UserID:        "user-test",
			})
			orch.Wait()

			results[idx] = rollbackResult{result: res, err: err}
		}(i)
	}

	// Release both goroutines simultaneously
	close(ready)
	wg.Wait()

	// Assert: exactly one succeeded, the other failed
	successCount := 0
	failureCount := 0
	for i, r := range results {
		if r.err == nil {
			successCount++
			t.Logf("goroutine %d: succeeded (rolled back %s, reactivated %s)",
				i, r.result.RolledBackRelease.ID, r.result.ReactivatedRelease.ID)
		} else {
			failureCount++
			t.Logf("goroutine %d: failed with: %v", i, r.err)
		}
	}

	if successCount != 1 {
		t.Fatalf("expected exactly 1 success, got %d (failures: %d)", successCount, failureCount)
	}
	if failureCount != 1 {
		t.Fatalf("expected exactly 1 failure, got %d (successes: %d)", failureCount, successCount)
	}

	// Assert: final state is consistent — exactly 1 ACTIVE release
	var activeCount int
	err = seed.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM releases
		 WHERE tenant_id = 'tenant-test' AND project_id = 'proj-test1234'
		   AND environment = 'production' AND status = 'ACTIVE'
		   AND id IN ($1, $2, $3)`,
		r1ID, r2ID, r3ID,
	).Scan(&activeCount)
	if err != nil {
		t.Fatalf("failed to count active releases: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 ACTIVE release, got %d", activeCount)
	}

	// Assert: R1 is no longer ACTIVE (it was rolled back by the winner)
	r1, err := releaseStore.GetByID(ctx, r1ID)
	if err != nil {
		t.Fatalf("failed to fetch R1: %v", err)
	}
	if r1.Status == string(statemachine.RPhActive) {
		t.Fatal("R1 should not be ACTIVE after rollback — lock serialization failed")
	}

	// Assert: environment_target points to the reactivated release
	target, err := envTargetStore.Get(ctx, "tenant-test", "proj-test1234", "production")
	if err != nil {
		t.Fatalf("failed to fetch environment_target: %v", err)
	}
	if target == nil {
		t.Fatal("environment_target not found after concurrent rollbacks")
	}
	if target.ActiveReleaseID == nil {
		t.Fatal("environment_target active_release_id is nil after rollback")
	}

	// The active release in env_target must match the single ACTIVE release in DB
	activeRelease, err := releaseStore.GetByID(ctx, *target.ActiveReleaseID)
	if err != nil {
		t.Fatalf("failed to fetch active release from env_target pointer: %v", err)
	}
	if activeRelease.Status != string(statemachine.RPhActive) {
		t.Fatalf("environment_target points to release %s with status %s (expected ACTIVE)",
			activeRelease.ID, activeRelease.Status)
	}

	t.Logf("concurrent rollback test passed: exactly 1 success, 1 failure, final state consistent (ACTIVE: %s)", activeRelease.ID)
}
