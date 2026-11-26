// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	loopruntimedb "github.com/getloopscale/loop-runtime/api/internal/db"
)

// Background loops sweep ACROSS tenants by definition — a retention pruner, a
// build reconciler and an idempotency cleaner all exist to look at every
// tenant's rows. Migration 000062 made the tenant tables fail-closed, and the
// request role (loopruntime_api) holds no cross-tenant policy: over that connection a
// sweep does not error, it returns nothing. Deleting zero rows and reconciling
// zero builds looks exactly like a quiet night.
//
// Measured 2026-08-14 against a real database with one audit_log row:
//
//	loopruntime (owner, no context)  -> 0 rows
//	loopruntime_api  (request role)  -> 0 rows
//	loopruntime_worker               -> 1 row   (audit_log_worker_cross_tenant)
//
// These tests pin BOTH halves: that the reach exists for loopruntime_worker, and that
// the guard refuses to let the process start background loops over a pool that
// does not have it. The second half is what turns a silent no-op into a boot
// failure someone can read.
//
// @see internal/db/rls_guard.go  — AssertBackgroundPoolCrossTenant
// @see migrations/000062_rls_fail_closed.up.sql — the named worker policies

func dsnOrSkip(t *testing.T, env string) string {
	t.Helper()
	dsn := os.Getenv(env)
	if dsn == "" {
		if auditRequired() {
			t.Fatalf("%s is unset and LOOPRUNTIME_RLS_AUDIT_REQUIRE=1 forbids skipping — "+
				"a green run here would assert nothing about background reach", env)
		}
		// Skipping on a machine without a database is the right decision; in CI,
		// LOOPRUNTIME_RLS_AUDIT_REQUIRE=1 turns this into a failure. Same tension tracked in:
		// https://github.com/getloopscale/loop-runtime/issues/289
		t.Skipf("%s is unset (set LOOPRUNTIME_RLS_AUDIT_REQUIRE=1 to make this a failure)", env)
	}
	return dsn
}

func poolFor(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		cancel()
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		cancel()
		pool.Close()
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(func() { cancel(); pool.Close() })
	return pool
}

// TestBackgroundPoolReach_WorkerRoleAccepted proves the sanctioned mechanism is
// intact: loopruntime_worker crosses tenants through a NAMED policy, so the guard lets
// the loops start.
func TestBackgroundPoolReach_WorkerRoleAccepted(t *testing.T) {
	pool := poolFor(t, dsnOrSkip(t, "LOOPRUNTIME_WORKER_DATABASE_URL"))

	if err := loopruntimedb.AssertBackgroundPoolCrossTenant(context.Background(), pool); err != nil {
		t.Fatalf("loopruntime_worker should be accepted for background work, got: %v", err)
	}
}

// TestBackgroundPoolReach_RequestRoleRefused is the one that matters. The
// request role is what DATABASE_URL points at in production (the request guard
// enforces it), and background loops used to inherit that same pool. Over it a
// sweep silently touches nothing.
func TestBackgroundPoolReach_RequestRoleRefused(t *testing.T) {
	pool := poolFor(t, dsnOrSkip(t, "LOOPRUNTIME_API_DATABASE_URL"))

	err := loopruntimedb.AssertBackgroundPoolCrossTenant(context.Background(), pool)
	if err == nil {
		t.Fatal("the request role holds no cross-tenant policy — the guard MUST refuse it; " +
			"accepting it is how a retention job deletes zero rows forever without a single log line")
	}
	for _, want := range []string{"loopruntime_worker", "LOOPRUNTIME_WORKER_DATABASE_URL"} {
		if !contains(err.Error(), want) {
			t.Errorf("the refusal must name the fix (%q), got: %v", want, err)
		}
	}
}

// TestBackgroundPoolReach_OwnerRefused closes the other door. The owner is not
// blocked by the policies in the way loopruntime_api is — it is blocked by having a
// privilege nobody declared, which is the shape migration 000063/000064 removed.
// Accepting it would trade a silent no-op for a silent bypass.
func TestBackgroundPoolReach_OwnerRefused(t *testing.T) {
	pool := poolFor(t, dsnOrSkip(t, "DATABASE_URL"))

	posture, err := loopruntimedb.MeasureRLSPosture(context.Background(), pool, "builds")
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if !posture.IsOwner && !posture.BypassRLS {
		// Nothing to assert: this test targets the OWNER's refusal, and this DSN is not
		// the owner. This is not the same tension as #289 (an audit that skips by
		// default) — here the test's condition simply does not exist in this database.
		// https://github.com/getloopscale/loop-runtime/issues/289
		t.Skipf("DATABASE_URL is not the owner (%s) — nothing to assert here", posture)
	}

	if err := loopruntimedb.AssertBackgroundPoolCrossTenant(context.Background(), pool); err == nil {
		t.Fatalf("the owner reaches every tenant through an UNNAMED privilege and MUST be refused: %s", posture)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
