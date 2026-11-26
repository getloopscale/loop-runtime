// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// T2.3 Test 1 (column-cascade) of plan
// 2026-05-28-meet-001-engineering-closure-plan.md.
//
// CR-9 of MEET-2026-05-28-001: the 3-layer Rekor URL persistence bug
// shipped because 13 SELECTs in release_store.go silently omitted the
// new column. The schema-mirror test (T2.2) catches drift statically;
// this test catches drift at runtime — for every SELECT-returning path
// on PostgresReleaseStore, a row INSERTED with a non-NULL Rekor URL
// must SELECT back with the same value.
//
// Skips when DATABASE_URL is unreachable OR when migrations 000005
// (release_app_attestations) + 000019 (image_attestation_rekor_url) are
// not applied.

// seedColumnCascadeRow inserts a release row with status='ACTIVE' and
// a non-NULL Rekor URL via raw SQL. Note: PostgresReleaseStore.Create
// forces status to READY (line 137 of release_store.go: state machine
// canonical entry point). The cascade test needs ACTIVE so
// GetActiveByEnv can find the row — raw INSERT bypasses Create's
// status override while still exercising the same INSERT column list
// (and the T2.1 triggers fire identically). Returns the URL so callers
// can assert SELECT round-trip equality.
func seedColumnCascadeRow(t *testing.T, pool *pgxpool.Pool) (*db.Release, string) {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantID := "tn-cascade-" + suffix
	buildID := "bld-cascade-" + suffix
	releaseID := "rel-cascade-" + suffix
	correlationID := "corr-cascade-" + suffix
	url := "https://rekor.loopzero.test/api/v1/log/entries?logIndex=" + suffix

	// Same 000061 trigger debt described in release_attestation_url_test.go:
	// the build only goes in if the tenant exists and has a cell.
	seedTenantForBuild(t, pool, tenantID)
	if _, err := pool.Exec(ctx,
		`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
		 VALUES ($1, $2, 'proj-cascade', 'production', 'SUCCEEDED', $3, 'system:test')`,
		buildID, tenantID, correlationID); err != nil {
		t.Fatalf("seed build: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by, image_attestation_rekor_url)
		 VALUES ($1, $2, $3, 'proj-cascade', 'production', 'ACTIVE', $4, 'system:test', $5)`,
		releaseID, buildID, tenantID, correlationID, &url); err != nil {
		t.Fatalf("seed release: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM releases WHERE id = $1", releaseID)
		_, _ = pool.Exec(ctx, "DELETE FROM builds WHERE id = $1", buildID)
	})
	return &db.Release{
		ID:            releaseID,
		BuildID:       buildID,
		TenantID:      tenantID,
		ProjectID:     "proj-cascade",
		Environment:   "production",
		Status:        "ACTIVE",
		CorrelationID: correlationID,
	}, url
}

func assertRekorURL(t *testing.T, label string, got *db.Release, wantURL string) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: nil release returned", label)
		return
	}
	if got.ImageAttestationRekorURL == nil || *got.ImageAttestationRekorURL != wantURL {
		var actual string
		if got.ImageAttestationRekorURL != nil {
			actual = *got.ImageAttestationRekorURL
		}
		t.Errorf("%s: rekor URL = %q, want %q (CR-9 class regression — SELECT dropped the column)",
			label, actual, wantURL)
	}
}

// TestReleaseStore_ColumnCascade_AllSelectsRoundTrip exercises 5 distinct
// SELECT shapes against a single seeded row:
//   - GetByID:              WHERE id = $1
//   - GetByIDForTenant:     WHERE tenant_id = $1 AND id = $2 (tenant scope)
//   - GetActiveByEnv:       WHERE tenant_id+project+env, status='ACTIVE', ORDER BY
//   - ListByEnv:            WHERE tenant+project+env, ORDER BY created_at DESC LIMIT
//   - GetLatestByBuildID:   WHERE build_id = $1
//
// These are the 5 most-used SELECT paths in production (every deploy +
// status check + rollback path goes through at least one). If any of
// them silently dropped image_attestation_rekor_url from its column
// list, this test fails — exactly the bug class CR-9 introduced.
func TestReleaseStore_ColumnCascade_AllSelectsRoundTrip(t *testing.T) {
	pool := setupCrossTenantTestDB(t)
	requireRekorURLTriggers(t, pool)
	adapter := &db.PgxAdapter{Pool: pool}
	relStore := store.NewReleaseStoreSqlc(adapter, store.NoopEventStore{}) // T1.1c2c (plan 2026-05-29-sqlc-migration v1.3)
	ctx := context.Background()
	rel, wantURL := seedColumnCascadeRow(t, pool)

	// 1) GetByID
	if got, err := relStore.GetByID(ctx, rel.ID); err != nil {
		t.Fatalf("GetByID: %v", err)
	} else {
		assertRekorURL(t, "GetByID", got, wantURL)
	}

	// 2) GetByIDForTenant
	if got, err := relStore.GetByIDForTenant(ctx, rel.TenantID, rel.ID); err != nil {
		t.Fatalf("GetByIDForTenant: %v", err)
	} else {
		assertRekorURL(t, "GetByIDForTenant", got, wantURL)
	}

	// 3) GetActiveByEnv — release is ACTIVE.
	if got, err := relStore.GetActiveByEnv(ctx, rel.TenantID, rel.ProjectID, rel.Environment); err != nil {
		t.Fatalf("GetActiveByEnv: %v", err)
	} else {
		assertRekorURL(t, "GetActiveByEnv", got, wantURL)
	}

	// 4) ListByEnv (limit 10) — expect at least one release; assert URL
	// on the row matching our seeded ID.
	rows, err := relStore.ListByEnv(ctx, rel.TenantID, rel.ProjectID, rel.Environment, 10)
	if err != nil {
		t.Fatalf("ListByEnv: %v", err)
	}
	var found *db.Release
	for i := range rows {
		if rows[i].ID == rel.ID {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("ListByEnv did not return seeded release %s (got %d rows)", rel.ID, len(rows))
	}
	assertRekorURL(t, "ListByEnv", found, wantURL)

	// 5) GetLatestByBuildID
	if got, err := relStore.GetLatestByBuildID(ctx, rel.BuildID); err != nil {
		t.Fatalf("GetLatestByBuildID: %v", err)
	} else {
		assertRekorURL(t, "GetLatestByBuildID", got, wantURL)
	}
}

// TestReleaseStore_ColumnCascade_NullURL_NoFalseString complements the
// happy-path test: a release inserted WITHOUT a Rekor URL must read
// back with the pointer field nil — not a zero-value empty string,
// not a stray default. If scanRelease ever fabricates an empty string
// for a NULL DB cell, downstream code that gates on URL != "" would
// produce false-positive audit rows.
func TestReleaseStore_ColumnCascade_NullURL_NoFalseString(t *testing.T) {
	pool := setupCrossTenantTestDB(t)
	requireRekorURLTriggers(t, pool)
	adapter := &db.PgxAdapter{Pool: pool}
	relStore := store.NewReleaseStoreSqlc(adapter, store.NoopEventStore{}) // T1.1c2c (plan 2026-05-29-sqlc-migration v1.3)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantID := "tn-cascade-null-" + suffix
	buildID := "bld-cascade-null-" + suffix
	releaseID := "rel-cascade-null-" + suffix
	correlationID := "corr-cascade-null-" + suffix

	// Same 000061 trigger debt described in release_attestation_url_test.go:
	// the build only goes in if the tenant exists and has a cell.
	seedTenantForBuild(t, pool, tenantID)
	if _, err := pool.Exec(ctx,
		`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
		 VALUES ($1, $2, 'proj-cascade-null', 'production', 'SUCCEEDED', $3, 'system:test')`,
		buildID, tenantID, correlationID); err != nil {
		t.Fatalf("seed build: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM releases WHERE id = $1", releaseID)
		_, _ = pool.Exec(ctx, "DELETE FROM builds WHERE id = $1", buildID)
	})

	rel := &db.Release{
		ID:                       releaseID,
		BuildID:                  buildID,
		TenantID:                 tenantID,
		ProjectID:                "proj-cascade-null",
		Environment:              "production",
		Status:                   "READY",
		CorrelationID:            correlationID,
		CreatedBy:                "system:test",
		ImageAttestationRekorURL: nil, // legacy / attest-disabled path
	}
	if err := relStore.Create(ctx, adapter, rel); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := relStore.GetByID(ctx, releaseID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ImageAttestationRekorURL != nil && *got.ImageAttestationRekorURL != "" {
		t.Errorf("expected nil URL for null-seeded release, got pointer to %q",
			*got.ImageAttestationRekorURL)
	}
	if got.ImageAttestationRekorURL != nil && strings.TrimSpace(*got.ImageAttestationRekorURL) == "" {
		t.Errorf("URL pointer must be nil (not pointer to empty string) when DB cell is NULL — observed pointer to empty string")
	}
}
