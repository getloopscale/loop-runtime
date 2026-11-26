// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package lock provides concurrency control for environment mutations.
//
// Every mutation on (tenant, project, environment) MUST use WithEnvironmentLock.
// This guarantees serialized access via PostgreSQL advisory locks.
package lock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// Tx is an alias for pgx.Tx, exposed so callers don't need to import pgx directly.
type Tx = pgx.Tx

// WithEnvironmentLock acquires a transaction-scoped advisory lock on (tenant, project, env)
// and executes fn within that transaction.
//
// WHY pg_advisory_xact_lock and NEVER pg_advisory_lock:
// Session-scoped locks (pg_advisory_lock) persist beyond the transaction. With connection
// pooling (pgxpool), the connection returns to the pool with the lock still held -> deadlocks.
// Transaction-scoped locks (pg_advisory_xact_lock) are released automatically on
// commit/rollback/disconnect. This is the ONLY safe variant with connection pools.
// See spec section 4.2.
//
// Lock key format: "{tenant}:{project}:{env}" — tenant is included for multi-tenant
// isolation (DA-4). hashtext() returns int4, compatible with pg_advisory_xact_lock.
// Collision risk is acceptable (~0.001% with 2000 envs): impact is false serialization
// (liveness), not data corruption (safety).
func WithEnvironmentLock[T any](ctx context.Context, pool *pgxpool.Pool, tenant, project, env string, fn func(tx Tx) (T, error)) (T, error) {
	// MEET-2026-03-30-020: Span wraps the entire lock lifecycle (acquire + fn + commit).
	// Instrumented AROUND the lock, NOT inside the critical section (C3, meeting condition).
	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.lock.acquire",
		trace.WithAttributes(
			attribute.String("loopzero.tenant", tenant),
			attribute.String("loopzero.project", project),
			attribute.String("loopzero.environment", env),
		))
	defer span.End()

	var zero T

	// Begin transaction with READ COMMITTED isolation (PostgreSQL default,
	// made explicit for clarity).
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	})
	if err != nil {
		return zero, fmt.Errorf("lock: begin transaction: %w", err)
	}

	// Ensure transaction is always finalized. On success we commit explicitly;
	// on any error or panic, rollback releases the advisory lock automatically.
	defer func() {
		// Rollback is a no-op if tx was already committed.
		_ = tx.Rollback(ctx)
	}()

	// Set lock timeout to 5 seconds (spec section 4.2).
	// SET LOCAL scopes the setting to this transaction only.
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '5s'"); err != nil {
		return zero, fmt.Errorf("lock: set lock_timeout: %w", err)
	}

	// MEET-2026-03-31-024: Set tenant context for RLS enforcement.
	// All queries inside this transaction will be scoped to this tenant.
	// This is defense-in-depth: even if a query forgets WHERE tenant_id,
	// the RLS policy enforces isolation.
	// Uses set_config() with parameterized value ($1) instead of SET LOCAL
	// to avoid string concatenation in SQL.
	if tenant != "" {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenant); err != nil {
			return zero, fmt.Errorf("lock: set tenant_id for RLS: %w", err)
		}
	}

	// Acquire the transaction-scoped advisory lock.
	// hashtext() converts the composite key string to int4 for pg_advisory_xact_lock.
	lockKey := fmt.Sprintf("%s:%s:%s", tenant, project, env)
	lockStart := time.Now()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", lockKey); err != nil {
		if isLockTimeoutError(err) {
			metrics.LockConflicts.WithLabelValues(tenant, env).Inc()
			span.SetStatus(codes.Error, "lock timeout")
			return zero, loopzeroerrors.ErrLockTimeout
		}
		return zero, fmt.Errorf("lock: acquire advisory lock: %w", err)
	}
	metrics.LockAcquired.WithLabelValues(tenant, env).Inc()

	// Execute the caller's function within the locked transaction.
	// On panic: defer above handles rollback; we recover and re-panic
	// so the lock is released but the panic propagates to the caller.
	var result T
	var fnPanicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				fnPanicked = true
				// Rollback happens in the outer defer. Re-panic.
				panic(r)
			}
		}()
		result, err = fn(tx)
	}()

	if fnPanicked {
		// Unreachable: the re-panic above prevents reaching here.
		// Kept for defensive clarity.
		return zero, fmt.Errorf("lock: panic in fn (should not reach)")
	}

	if err != nil {
		// fn returned an error: rollback happens in defer, propagate the error.
		return zero, err
	}

	// Commit the transaction. The advisory lock is released on commit.
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("lock: commit transaction: %w", err)
	}

	metrics.LockDuration.WithLabelValues(tenant, env).Observe(time.Since(lockStart).Seconds())
	return result, nil
}

// isLockTimeoutError checks if the error is a PostgreSQL lock_not_available error (55P03).
// This is the error code returned when SET LOCAL lock_timeout expires while waiting
// for pg_advisory_xact_lock.
func isLockTimeoutError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "55P03"
	}
	return false
}
