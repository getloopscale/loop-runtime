// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package lock_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/loopzero/loopzero/api/internal/lock"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// testPool creates a pgxpool.Pool for testing. Requires a running PostgreSQL
// instance. Set DATABASE_URL env var or defaults to localhost.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/loopzero_test?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
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

func TestWithEnvironmentLock_HappyPath(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// Arrange & Act: execute fn inside the lock, return a typed result.
	result, err := lock.WithEnvironmentLock(ctx, pool, "tenant-1", "my-project", "production",
		func(tx lock.Tx) (string, error) {
			return "build-123", nil
		},
	)

	// Assert: fn result is returned, no error, transaction committed.
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result != "build-123" {
		t.Fatalf("expected result 'build-123', got '%s'", result)
	}
}

func TestWithEnvironmentLock_ConcurrentSameKey(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// Arrange: channels to synchronize goroutines.
	lockAcquired := make(chan struct{})
	releaseA := make(chan struct{})

	var wg sync.WaitGroup
	var orderMu sync.Mutex
	var executionOrder []string

	// Act: goroutine A acquires lock and holds it.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = lock.WithEnvironmentLock(ctx, pool, "tenant-1", "project-1", "staging",
			func(tx lock.Tx) (struct{}, error) {
				orderMu.Lock()
				executionOrder = append(executionOrder, "A-start")
				orderMu.Unlock()

				close(lockAcquired) // signal that A has the lock
				<-releaseA          // wait for signal to release

				orderMu.Lock()
				executionOrder = append(executionOrder, "A-end")
				orderMu.Unlock()
				return struct{}{}, nil
			},
		)
	}()

	// Wait for A to acquire the lock.
	<-lockAcquired

	// Act: goroutine B tries to acquire the same lock.
	// Use a longer timeout context to allow B to wait for A.
	ctxB, cancelB := context.WithTimeout(ctx, 10*time.Second)
	defer cancelB()

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = lock.WithEnvironmentLock(ctxB, pool, "tenant-1", "project-1", "staging",
			func(tx lock.Tx) (struct{}, error) {
				orderMu.Lock()
				executionOrder = append(executionOrder, "B")
				orderMu.Unlock()
				return struct{}{}, nil
			},
		)
	}()

	// Give B time to start waiting for the lock.
	// This sleep is intentional: we need goroutine B to be blocked on the
	// advisory lock before we release A. There is no observable signal for
	// "B is waiting on the lock" — only the PG backend status could tell us,
	// and that would add a test-only query. 200ms is a safe margin.
	time.Sleep(200 * time.Millisecond) // intentional: waiting for goroutine to block on PG advisory lock

	// Release A.
	close(releaseA)

	wg.Wait()

	// Assert: A completed before B started.
	orderMu.Lock()
	defer orderMu.Unlock()

	if len(executionOrder) != 3 {
		t.Fatalf("expected 3 execution entries, got %d: %v", len(executionOrder), executionOrder)
	}
	if executionOrder[0] != "A-start" || executionOrder[1] != "A-end" || executionOrder[2] != "B" {
		t.Fatalf("expected [A-start, A-end, B], got %v", executionOrder)
	}
}

func TestWithEnvironmentLock_Timeout(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// Arrange: goroutine A acquires lock and holds it beyond 5s timeout.
	lockAcquired := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		_, _ = lock.WithEnvironmentLock(ctx, pool, "tenant-1", "project-x", "prod",
			func(tx lock.Tx) (struct{}, error) {
				close(lockAcquired)
				// intentional: hold lock for longer than the 5s timeout to test lock acquisition timeout behavior
				time.Sleep(8 * time.Second)
				return struct{}{}, nil
			},
		)
	}()

	<-lockAcquired

	// Act: goroutine B tries to acquire the same lock -> should get timeout error.
	_, err := lock.WithEnvironmentLock(ctx, pool, "tenant-1", "project-x", "prod",
		func(tx lock.Tx) (string, error) {
			return "should-not-reach", nil
		},
	)

	// Assert: error is ErrLockTimeout.
	if err == nil {
		t.Fatal("expected ErrLockTimeout, got nil")
	}
	if !errors.Is(err, loopzeroerrors.ErrLockTimeout) {
		t.Fatalf("expected ErrLockTimeout, got: %v", err)
	}

	// Cleanup: wait for A to finish.
	<-done
}

func TestWithEnvironmentLock_ErrorInFn(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	fnErr := loopzeroerrors.NewLoopZeroError("TEST_ERROR", "something went wrong", 500)

	// Act: fn returns an error.
	_, err := lock.WithEnvironmentLock(ctx, pool, "tenant-1", "project-err", "dev",
		func(tx lock.Tx) (string, error) {
			return "", fnErr
		},
	)

	// Assert: error is propagated, transaction rolled back (lock released).
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, fnErr) {
		t.Fatalf("expected fnErr, got: %v", err)
	}

	// Verify lock is released by acquiring it again immediately.
	_, err = lock.WithEnvironmentLock(ctx, pool, "tenant-1", "project-err", "dev",
		func(tx lock.Tx) (string, error) {
			return "ok", nil
		},
	)
	if err != nil {
		t.Fatalf("expected lock to be released after error, but got: %v", err)
	}
}

func TestWithEnvironmentLock_DifferentTenantsSameProjectEnv(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// Arrange: two different tenants, same project and environment.
	// Both should acquire their locks simultaneously without conflict.
	lockA := make(chan struct{})
	lockB := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup

	// Act: both goroutines acquire locks at the same time.
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = lock.WithEnvironmentLock(ctx, pool, "tenant-alpha", "shared-project", "prod",
			func(tx lock.Tx) (struct{}, error) {
				close(lockA)
				<-release
				return struct{}{}, nil
			},
		)
	}()

	go func() {
		defer wg.Done()
		_, _ = lock.WithEnvironmentLock(ctx, pool, "tenant-beta", "shared-project", "prod",
			func(tx lock.Tx) (struct{}, error) {
				close(lockB)
				<-release
				return struct{}{}, nil
			},
		)
	}()

	// Assert: both acquired locks concurrently (within a reasonable timeframe).
	select {
	case <-lockA:
	case <-time.After(5 * time.Second):
		t.Fatal("tenant-alpha failed to acquire lock within 5s")
	}
	select {
	case <-lockB:
	case <-time.After(5 * time.Second):
		t.Fatal("tenant-beta failed to acquire lock within 5s")
	}

	// Both have the lock simultaneously — test passes.
	close(release)
	wg.Wait()
}
