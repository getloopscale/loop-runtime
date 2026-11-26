// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/generated"
)

func TestLatestReleaseStatuses_NoBuildsAsksNothing(t *testing.T) {
	// The early return comes before the pool is touched; a zero store is safe.
	got, err := (&ReleaseStoreSqlc{}).LatestReleaseStatuses(context.Background(), "tn-a", nil)
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want an empty map", got, err)
	}
}

// TestLatestReleaseStatuses_RealPG runs the query against a Postgres loaded
// with schema.gen.sql (LOOPZERO_E2E_DATABASE_URL), inside a transaction it
// rolls back.
func TestLatestReleaseStatuses_RealPG(t *testing.T) {
	dsn := os.Getenv("LOOPZERO_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("LOOPZERO_E2E_DATABASE_URL not set: real-PG query test skipped")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	build := func(id, tenant, env string) {
		t.Helper()
		if _, err := tx.Exec(ctx, `INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by, commit_sha, cell_id)
			VALUES ($1, $2, 'web', $3, 'SUCCEEDED', 'corr', 'user', 'c1', 'cell-0')`, id, tenant, env); err != nil {
			t.Fatalf("seed build %s: %v", id, err)
		}
	}
	base := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	release := func(id, buildID, tenant, env, status string, at time.Duration) {
		t.Helper()
		if _, err := tx.Exec(ctx, `INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by, cell_id, created_at)
			VALUES ($1, $2, $3, 'web', $4, $5, 'corr', 'user', 'cell-0', $6)`, id, buildID, tenant, env, status, base.Add(at)); err != nil {
			t.Fatalf("seed release %s: %v", id, err)
		}
	}

	build("bld-rs-failed", "tn-a", "qa")
	release("rel-rs-1", "bld-rs-failed", "tn-a", "qa", "SYNC_FAILED", 0)

	// Two releases of one build: the later one is the answer.
	build("bld-rs-twice", "tn-a", "qa")
	release("rel-rs-2", "bld-rs-twice", "tn-a", "qa", "SYNC_FAILED", 0)
	release("rel-rs-3", "bld-rs-twice", "tn-a", "qa", "SUPERSEDED", time.Hour)

	// Promoted to production later: that release is not the build's own deploy.
	build("bld-rs-promoted", "tn-a", "qa")
	release("rel-rs-4", "bld-rs-promoted", "tn-a", "qa", "SUPERSEDED", 0)
	release("rel-rs-5", "bld-rs-promoted", "tn-a", "production", "CANCELLED", 2*time.Hour)

	build("bld-rs-norelease", "tn-a", "qa")
	build("bld-rs-other-tenant", "tn-b", "qa")
	release("rel-rs-6", "bld-rs-other-tenant", "tn-b", "qa", "SYNC_FAILED", 0)

	s := &ReleaseStoreSqlc{q: generated.New()}
	got, err := s.latestReleaseStatuses(ctx, &db.PgxTxAdapter{Tx: tx}, "tn-a", []string{
		"bld-rs-failed", "bld-rs-twice", "bld-rs-promoted", "bld-rs-norelease", "bld-rs-other-tenant", "bld-rs-absent",
	})
	if err != nil {
		t.Fatalf("latestReleaseStatuses: %v", err)
	}

	want := map[string]string{"bld-rs-failed": "SYNC_FAILED", "bld-rs-twice": "SUPERSEDED", "bld-rs-promoted": "SUPERSEDED"}
	if len(got) != len(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	for id, status := range want {
		if got[id] != status {
			t.Errorf("%s = %q, want %q", id, got[id], status)
		}
	}
}
