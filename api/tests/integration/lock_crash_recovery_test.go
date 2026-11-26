// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package integration tests advisory lock crash recovery behavior.
//
// These tests prove that pg_advisory_xact_lock releases automatically when:
// 1. A connection is terminated abruptly (simulating process crash)
// 2. A panic occurs inside the locked function
//
// The "error in fn -> rollback -> lock released" scenario is already covered
// in api/internal/lock/environment_lock_test.go (TestWithEnvironmentLock_ErrorInFn).
//
// These tests require a real PostgreSQL instance. They skip gracefully if
// DATABASE_URL is not set and the default connection fails.
package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loop-runtime/api/internal/lock"
)

// dbURL returns the PostgreSQL connection string for tests.
func dbURL() string {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://postgres:postgres@localhost:5432/loop_runtime_test?sslmode=disable"
	}
	return url
}

// testPool creates a pgxpool.Pool for testing. Skips if PostgreSQL is unreachable.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL())
	if err != nil {
		t.Skipf("skipping: cannot connect to PostgreSQL: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping: cannot ping PostgreSQL: %v", err)
	}

	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestLockReleasedOnConnectionCrash(t *testing.T) {
	// This test proves that when a PostgreSQL backend holding pg_advisory_xact_lock
	// is terminated (simulating process crash), the lock is released immediately.
	// Another connection can acquire the same lock without waiting for the 5s timeout.
	//
	// Strategy: use a raw pgx connection to acquire the lock and record its PID,
	// then use pg_terminate_backend() from a separate connection to kill it.
	// This is more realistic than closing a pool — it simulates what happens
	// when the API process dies and PostgreSQL detects the dead socket.

	mainPool := testPool(t)
	ctx := context.Background()

	// Use a unique key to avoid interference with other tests.
	tenant := "crash-test-tenant"
	project := "crash-test-project"
	env := "crash-test-env"
	lockKey := fmt.Sprintf("%s:%s:%s", tenant, project, env)

	// Arrange: open a raw connection (not from pool) to simulate the crasher.
	crasherConn, err := pgx.Connect(ctx, dbURL())
	if err != nil {
		t.Skipf("skipping: cannot open crasher connection: %v", err)
	}

	// Get the PID of the crasher connection before acquiring the lock.
	var crasherPID int
	err = crasherConn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&crasherPID)
	if err != nil {
		_ = crasherConn.Close(ctx)
		t.Fatalf("failed to get crasher PID: %v", err)
	}

	// Start a transaction and acquire the advisory lock on the crasher connection.
	crasherTx, err := crasherConn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		_ = crasherConn.Close(ctx)
		t.Fatalf("failed to begin crasher transaction: %v", err)
	}

	_, err = crasherTx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", lockKey)
	if err != nil {
		_ = crasherTx.Rollback(ctx)
		_ = crasherConn.Close(ctx)
		t.Fatalf("failed to acquire advisory lock on crasher: %v", err)
	}

	t.Logf("crasher PID %d holds advisory lock on key '%s'", crasherPID, lockKey)

	// Act: kill the crasher backend from the main pool, simulating a crash.
	// pg_terminate_backend sends SIGTERM to the backend process, which causes
	// PostgreSQL to roll back the transaction and release all locks.
	var terminated bool
	err = mainPool.QueryRow(ctx, "SELECT pg_terminate_backend($1)", crasherPID).Scan(&terminated)
	if err != nil {
		t.Fatalf("failed to terminate crasher backend: %v", err)
	}
	if !terminated {
		t.Fatal("pg_terminate_backend returned false — backend was not terminated")
	}

	t.Logf("terminated crasher PID %d", crasherPID)

	// Act: immediately try to acquire the same lock from the main pool.
	// If the lock was released by the termination, this should succeed fast.
	start := time.Now()

	result, acquireErr := lock.WithEnvironmentLock(ctx, mainPool, tenant, project, env,
		func(tx lock.Tx) (string, error) {
			return "acquired-after-crash", nil
		},
	)
	elapsed := time.Since(start)

	// Assert: lock was acquired without error.
	if acquireErr != nil {
		t.Fatalf("expected lock acquisition after crash, got error: %v", acquireErr)
	}
	if result != "acquired-after-crash" {
		t.Fatalf("expected result 'acquired-after-crash', got '%s'", result)
	}

	// Assert: acquisition was fast (not waiting for the 5s lock timeout).
	// If the lock was NOT released, the caller would wait 5s and get ErrLockTimeout.
	// We allow 2 seconds generously — in practice it takes milliseconds.
	if elapsed > 2*time.Second {
		t.Fatalf("lock acquisition after crash took %v — expected < 2s (lock may not have been released)", elapsed)
	}

	t.Logf("lock acquired %v after crash (confirms auto-release on backend termination)", elapsed)

	// Cleanup: close the crasher connection (already dead, but be explicit).
	_ = crasherConn.Close(ctx)
}

func TestLockReleasedOnPanic(t *testing.T) {
	// This test proves that when the function inside WithEnvironmentLock panics,
	// the transaction is rolled back (via defer) and the advisory lock is released.
	// The next caller can acquire the same lock immediately.

	pool := testPool(t)
	ctx := context.Background()

	tenant := "panic-test-tenant"
	project := "panic-test-project"
	env := "panic-test-env"

	// Arrange & Act: call WithEnvironmentLock with a function that panics.
	panicMsg := "intentional panic for crash recovery test"
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected panic to propagate, but no panic occurred")
			}
			recovered, ok := r.(string)
			if !ok {
				t.Fatalf("expected panic value to be string, got %T: %v", r, r)
			}
			if recovered != panicMsg {
				t.Fatalf("expected panic message '%s', got '%s'", panicMsg, recovered)
			}
		}()

		_, _ = lock.WithEnvironmentLock(ctx, pool, tenant, project, env,
			func(tx lock.Tx) (string, error) {
				panic(panicMsg)
			},
		)
	}()

	// Act: try to acquire the same lock immediately after the panic.
	start := time.Now()
	result, err := lock.WithEnvironmentLock(ctx, pool, tenant, project, env,
		func(tx lock.Tx) (string, error) {
			return "acquired-after-panic", nil
		},
	)
	elapsed := time.Since(start)

	// Assert: lock was acquired without error, meaning it was released on panic.
	if err != nil {
		t.Fatalf("expected lock to be released after panic, got error: %v", err)
	}
	if result != "acquired-after-panic" {
		t.Fatalf("expected result 'acquired-after-panic', got '%s'", result)
	}

	// Assert: acquisition was immediate (well under the 5s timeout).
	if elapsed > 1*time.Second {
		t.Fatalf("lock acquisition after panic took %v — expected < 1s", elapsed)
	}

	t.Logf("lock acquired %v after panic (confirms auto-release on rollback)", elapsed)
}

func TestLockReleasedOnRollback_ThenNextOperationSucceeds(t *testing.T) {
	// This test proves the full cycle: error -> rollback -> lock released -> next
	// operation acquires lock and commits successfully with a real DB query.
	// While TestWithEnvironmentLock_ErrorInFn in the unit tests covers the basic
	// error+re-acquire case, this integration test verifies that a meaningful
	// database operation succeeds in the second call, proving the transaction
	// state is fully clean.

	pool := testPool(t)
	ctx := context.Background()

	tenant := "rollback-test-tenant"
	project := "rollback-test-project"
	env := "rollback-test-env"

	// Arrange & Act: first call performs a DB operation then returns an error.
	_, err := lock.WithEnvironmentLock(ctx, pool, tenant, project, env,
		func(tx lock.Tx) (string, error) {
			// Perform a database operation inside the transaction.
			_, execErr := tx.Exec(ctx, "SELECT 1")
			if execErr != nil {
				t.Fatalf("unexpected error in first call: %v", execErr)
			}
			// Return an error to trigger rollback.
			return "", fmt.Errorf("simulated business logic failure")
		},
	)
	if err == nil {
		t.Fatal("expected error from first call, got nil")
	}

	// Act: second call on the same key should acquire lock and succeed.
	start := time.Now()
	result, err := lock.WithEnvironmentLock(ctx, pool, tenant, project, env,
		func(tx lock.Tx) (string, error) {
			// Perform a real query to prove the transaction works.
			var value int
			queryErr := tx.QueryRow(ctx, "SELECT 42").Scan(&value)
			if queryErr != nil {
				return "", fmt.Errorf("query failed: %w", queryErr)
			}
			return fmt.Sprintf("result-%d", value), nil
		},
	)
	elapsed := time.Since(start)

	// Assert: second call succeeded.
	if err != nil {
		t.Fatalf("expected second call to succeed, got error: %v", err)
	}
	if result != "result-42" {
		t.Fatalf("expected 'result-42', got '%s'", result)
	}

	// Assert: acquisition was immediate.
	if elapsed > 1*time.Second {
		t.Fatalf("second lock acquisition took %v — expected < 1s", elapsed)
	}

	t.Logf("second lock acquired %v after rollback (confirms clean state)", elapsed)
}
