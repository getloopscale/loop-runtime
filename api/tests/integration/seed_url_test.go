// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package integration

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedDBURL is the connection a FIXTURE should use, which is not the same as
// the connection a REQUEST should use.
//
// Since migration 000062 the tenant tables are fail-closed, and 000064 removed
// the owner's standing `*_owner_maintenance` bypass. A fixture that seeds two
// tenants to prove a cross-tenant property is doing cross-tenant work by
// definition — the same job the background loops do — so it connects the same
// way they do: as `loopruntime_worker`, whose reach is the NAMED
// `*_worker_cross_tenant` policies (migration 000062).
//
// The alternative would be to restore a broad standing privilege for the owner
// so fixtures keep working. That is the bypass this whole line of work removed,
// and re-adding it for tests would make the suite green over a posture
// production does not have — which is the exact failure the 2026-08-14 audit
// found (a tenant-isolation test asserting isolation over a connection that
// structurally could not be isolated).
//
// Falls back to DATABASE_URL when the worker DSN is unset, so a developer
// machine without the role split still runs the suite.
func seedDBURL() string {
	if url := os.Getenv("LOOPRUNTIME_WORKER_DATABASE_URL"); url != "" {
		return url
	}
	return dbURL()
}

// seedPool is the cross-tenant fixture pool (see seedDBURL). Like testPool it
// skips when PostgreSQL is unreachable.
func seedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_ = testPool(t)
	return poolFor(t, seedDBURL())
}

// ownerDBURL is the connection for the one kind of fixture row the engine's
// roles are not meant to manage: an Account. The engine only ever creates
// Accounts (the bootstrap path), so the cross-tenant role holds SELECT and
// INSERT on `accounts` and no DELETE (migration 000068 — least privilege
// derived from the engine's SQL). `accounts` carries no Row Level Security, so
// its owner writes and removes the rows a test needs through an ordinary table
// privilege — not the RLS bypass seedDBURL explains is gone. Seeding them as
// the owner also keeps every fixture independent of the grants the tests in
// worker_role_grants_test.go are there to prove.
//
// Falls back to seedDBURL when LOOPRUNTIME_OWNER_DATABASE_URL is unset, which is
// correct on a machine without the role split.
func ownerDBURL() string {
	if url := os.Getenv("LOOPRUNTIME_OWNER_DATABASE_URL"); url != "" {
		return url
	}
	return seedDBURL()
}

// execAsAccountsOwner runs one fixture statement on `accounts` as the table
// owner (see ownerDBURL). Every other fixture row goes through seedDBURL.
func execAsAccountsOwner(ctx context.Context, query string, args ...any) error {
	pool, err := pgxpool.New(ctx, ownerDBURL())
	if err != nil {
		return fmt.Errorf("connect as the accounts owner: %w", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("fixture accounts statement as the owner: %w", err)
	}
	return nil
}
