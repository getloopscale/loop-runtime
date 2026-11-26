// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// T2.1 of plan 2026-05-28-meet-001-engineering-closure-plan.md
// CR-4 of MEET-2026-05-28-001 — releases.image_attestation_rekor_url
// gains a BEFORE UPDATE trigger raising EXCEPTION on mutation once
// set, plus an AFTER INSERT trigger that emits an audit_log row
// when a non-NULL Rekor URL is persisted.
//
// These tests connect to a real PostgreSQL instance via DATABASE_URL
// (default postgres://postgres:postgres@localhost:5432/loop_runtime_test) and
// skip gracefully when PG is unreachable or migration 000020 has not
// been applied. The skip-on-pre-migration check is intentional: it
// gives the RED-first signal during development without blocking
// the wider suite when migrations are pending.

// requireTrigger skips the test if migration 000020 has not been
// applied (the trigger doesn't exist yet). Detected by querying
// pg_trigger for tgname='releases_attestation_url_immutable'.
func requireRekorURLTriggers(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var count int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_trigger
		WHERE tgname IN (
			'releases_attestation_url_immutable',
			'releases_attestation_url_audit'
		)`).Scan(&count)
	if err != nil {
		t.Skipf("cannot query pg_trigger: %v", err)
	}
	if count < 2 {
		t.Skipf("migration 000020 not applied (found %d/2 triggers); run `task db:migrate`", count)
	}
}

// seedReleaseRow inserts the minimal upstream chain (build → release row)
// needed for the trigger tests. Returns the release ID + correlation ID
// used for the INSERT so callers can assert against them.
func seedReleaseRow(t *testing.T, pool *pgxpool.Pool, rekorURL *string) (releaseID, correlationID, tenantID string) {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantID = "tn-rekor-" + suffix
	buildID := "bld-rekor-" + suffix
	releaseID = "rel-rekor-" + suffix
	correlationID = "corr-rekor-" + suffix

	// The tenant must EXIST before the build: migration 000061 added a trigger
	// that resolves `builds.cell_id` from `tenants.cell_id` and fails loudly
	// when the tenant is not there. This fixture predates that trigger and was
	// inserting the build directly — the debt the isolation job's comment in CI
	// describes as "fixtures that predate the cell_id trigger".
	_, err := pool.Exec(ctx,
		`INSERT INTO users (id,email,display_name,username) VALUES ($1,$1||'@rekor.test','R',$1)
		 ON CONFLICT (id) DO NOTHING`, "u-"+suffix)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO accounts (id,display_name,plan,owner_id) VALUES ($1,'R','starter',$2)
		 ON CONFLICT (id) DO NOTHING`, "acct-"+suffix, "u-"+suffix)
	if err != nil {
		t.Fatalf("seed account: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO tenants (id,display_name,slug,owner_id,account_id,cell_id)
		 VALUES ($1,$1,$1,$2,$3,'cell-0') ON CONFLICT (id) DO NOTHING`,
		tenantID, "u-"+suffix, "acct-"+suffix)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// Builds row (NOT NULL FK from releases.build_id).
	_, err = pool.Exec(ctx,
		`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
		 VALUES ($1, $2, 'proj-rekor', 'production', 'SUCCEEDED', $3, 'system:test')`,
		buildID, tenantID, correlationID)
	if err != nil {
		t.Fatalf("seed build: %v", err)
	}

	// Release row with optional Rekor URL.
	_, err = pool.Exec(ctx,
		`INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by, image_attestation_rekor_url)
		 VALUES ($1, $2, $3, 'proj-rekor', 'production', 'READY', $4, 'system:test', $5)`,
		releaseID, buildID, tenantID, correlationID, rekorURL)
	if err != nil {
		t.Fatalf("seed release: %v", err)
	}

	t.Cleanup(func() {
		// Best-effort cleanup; trigger-emitted audit_log rows are kept
		// (they aren't FK-bound to releases). Test isolation is by
		// suffix-uniqued IDs.
		_, _ = pool.Exec(ctx, "DELETE FROM releases WHERE id = $1", releaseID)
		_, _ = pool.Exec(ctx, "DELETE FROM builds WHERE id = $1", buildID)
	})
	return
}

// TestReleaseStore_InsertWithRekorURL_WritesAuditLog (T2.1 RED #1):
// AFTER INSERT trigger emits an audit_log row with the expected fields.
func TestReleaseStore_InsertWithRekorURL_WritesAuditLog(t *testing.T) {
	pool := setupCrossTenantTestDB(t)
	requireRekorURLTriggers(t, pool)
	ctx := context.Background()

	url := "https://rekor.loop-runtime.test/api/v1/log/entries?logIndex=42"
	releaseID, correlationID, tenantID := seedReleaseRow(t, pool, &url)

	var (
		gotAction      string
		gotResourceID  string
		gotTenantID    string
		gotActor       string
		gotResourceTyp string
		gotCorr        *string
		gotMetadata    string
	)
	err := pool.QueryRow(ctx, `
		SELECT action, resource_id, tenant_id, actor, resource_type, correlation_id, metadata::text
		FROM audit_log
		WHERE resource_id = $1 AND action = 'release.attestation_url_set'
		ORDER BY id DESC LIMIT 1`, releaseID).
		Scan(&gotAction, &gotResourceID, &gotTenantID, &gotActor, &gotResourceTyp, &gotCorr, &gotMetadata)
	if err != nil {
		t.Fatalf("audit_log row not found for release %s: %v", releaseID, err)
	}
	if gotAction != "release.attestation_url_set" {
		t.Errorf("action = %q, want release.attestation_url_set", gotAction)
	}
	if gotTenantID != tenantID {
		t.Errorf("tenant_id = %q, want %q", gotTenantID, tenantID)
	}
	if gotActor != "system:release-attestation" {
		t.Errorf("actor = %q, want system:release-attestation", gotActor)
	}
	if gotResourceTyp != "release" {
		t.Errorf("resource_type = %q, want release", gotResourceTyp)
	}
	if gotCorr == nil || *gotCorr != correlationID {
		t.Errorf("correlation_id = %v, want %q", gotCorr, correlationID)
	}
	if !strings.Contains(gotMetadata, "rekor_url") || !strings.Contains(gotMetadata, "logIndex=42") {
		t.Errorf("metadata = %q, want it to contain rekor_url + the log index", gotMetadata)
	}
}

// TestReleaseStore_InsertNullRekorURL_NoAuditLog (T2.1 RED #2):
// AFTER INSERT trigger does NOT emit an audit_log row when the
// Rekor URL is NULL (legacy / attest-disabled path).
func TestReleaseStore_InsertNullRekorURL_NoAuditLog(t *testing.T) {
	pool := setupCrossTenantTestDB(t)
	requireRekorURLTriggers(t, pool)
	ctx := context.Background()

	releaseID, _, _ := seedReleaseRow(t, pool, nil)

	var count int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_log
		WHERE resource_id = $1 AND action = 'release.attestation_url_set'`, releaseID).
		Scan(&count)
	if err != nil {
		t.Fatalf("count audit_log: %v", err)
	}
	if count != 0 {
		t.Errorf("audit rows for NULL-URL release = %d, want 0", count)
	}
}

// TestReleaseStore_UpdateSameRekorURL_NoException (T2.1 RED #3):
// BEFORE UPDATE trigger PASSES when the UPDATE re-asserts the same
// value (IS DISTINCT FROM is FALSE). This is important so idempotent
// retry-paths don't trip the write-once gate.
func TestReleaseStore_UpdateSameRekorURL_NoException(t *testing.T) {
	pool := setupCrossTenantTestDB(t)
	requireRekorURLTriggers(t, pool)
	ctx := context.Background()

	url := "https://rekor.loop-runtime.test/api/v1/log/entries?logIndex=7"
	releaseID, _, _ := seedReleaseRow(t, pool, &url)

	// Re-assert the same value via an unrelated UPDATE that touches
	// the row (status change). The BEFORE UPDATE trigger fires on
	// every UPDATE, so the same-value branch must short-circuit.
	_, err := pool.Exec(ctx,
		`UPDATE releases SET status = 'PENDING' WHERE id = $1`, releaseID)
	if err != nil {
		t.Fatalf("UPDATE not touching rekor url unexpectedly failed: %v", err)
	}
	// Explicit same-value rewrite — also must pass.
	_, err = pool.Exec(ctx,
		`UPDATE releases SET image_attestation_rekor_url = $1 WHERE id = $2`,
		&url, releaseID)
	if err != nil {
		t.Fatalf("UPDATE re-asserting the same rekor URL unexpectedly failed: %v", err)
	}
}

// TestReleaseStore_UpdateDifferentRekorURL_RaisesException (T2.1 RED #4):
// BEFORE UPDATE trigger raises a check_violation when the UPDATE tries
// to change the URL to a different non-NULL value (or to NULL — IS
// DISTINCT FROM treats NULL as distinct).
func TestReleaseStore_UpdateDifferentRekorURL_RaisesException(t *testing.T) {
	pool := setupCrossTenantTestDB(t)
	requireRekorURLTriggers(t, pool)
	ctx := context.Background()

	orig := "https://rekor.loop-runtime.test/api/v1/log/entries?logIndex=100"
	releaseID, _, _ := seedReleaseRow(t, pool, &orig)

	attacker := "https://attacker.example/log/entries?logIndex=100"
	_, err := pool.Exec(ctx,
		`UPDATE releases SET image_attestation_rekor_url = $1 WHERE id = $2`,
		&attacker, releaseID)
	if err == nil {
		t.Fatal("UPDATE rewriting rekor URL must FAIL — write-once trigger missing or wrong")
	}
	if !strings.Contains(err.Error(), "write-once") {
		t.Errorf("expected error mentioning write-once, got: %v", err)
	}

	// Set-to-NULL is also rewriting and must fail (NULL IS DISTINCT FROM 'x').
	_, err = pool.Exec(ctx,
		`UPDATE releases SET image_attestation_rekor_url = NULL WHERE id = $1`,
		releaseID)
	if err == nil {
		t.Fatal("UPDATE clearing rekor URL must FAIL — write-once trigger missing or wrong")
	}
}

// TestReleaseStore_UpdateFirstSetFromNull_Allowed (T0.3 EC-6 of plan
// 2026-06-01-p1-security-hardening): the BEFORE UPDATE trigger has an
// `OLD IS NOT NULL` guard, so a release INSERTed with NULL rekor URL can
// later be UPDATEd to a non-NULL value exactly once (the first-set path
// via UPDATE). This documents the intentional null→non-null transition
// path used by the build_reconcile_complete flow when attestation is
// enriched post-INSERT.
func TestReleaseStore_UpdateFirstSetFromNull_Allowed(t *testing.T) {
	pool := setupCrossTenantTestDB(t)
	requireRekorURLTriggers(t, pool)
	ctx := context.Background()

	// Insert with NULL rekor URL (legacy / pre-attestation path).
	releaseID, _, _ := seedReleaseRow(t, pool, nil)

	// First-set via UPDATE: trigger's `OLD IS NOT NULL` guard skips the
	// raise, so this succeeds.
	firstURL := "https://rekor.loop-runtime.test/api/v1/log/entries?logIndex=200"
	_, err := pool.Exec(ctx,
		`UPDATE releases SET image_attestation_rekor_url = $1 WHERE id = $2`,
		&firstURL, releaseID)
	if err != nil {
		t.Fatalf("first-set via UPDATE from NULL must succeed (OLD IS NOT NULL guard); got: %v", err)
	}

	// Verify the value is now persisted.
	var got *string
	row := pool.QueryRow(ctx,
		`SELECT image_attestation_rekor_url FROM releases WHERE id = $1`, releaseID)
	if err := row.Scan(&got); err != nil {
		t.Fatalf("SELECT after first-set: %v", err)
	}
	if got == nil || *got != firstURL {
		t.Errorf("first-set value mismatch: got %v want %s", got, firstURL)
	}

	// And the trigger should now reject subsequent mutations (write-once
	// behaviour kicks in once OLD is non-NULL).
	other := "https://attacker.example/log/entries?logIndex=999"
	_, err = pool.Exec(ctx,
		`UPDATE releases SET image_attestation_rekor_url = $1 WHERE id = $2`,
		&other, releaseID)
	if err == nil {
		t.Fatal("second UPDATE after first-set must FAIL — write-once after non-NULL")
	}
}

// TestReleaseStore_InsertRolledBack_AuditLogNotPersisted — EC-6 of plan
// 2026-06-01-sprint-a-p1-hotfixes. The AFTER INSERT trigger
// `audit_attestation_url_set` fires inside the parent transaction. If the
// transaction ROLLBACKs (e.g., FK violation downstream, application error
// before COMMIT), the audit_log row MUST roll back too — leaving no orphan
// audit entry referencing a release that never persisted.
//
// Regression guard against future "audit_log via outbox" refactors that
// would decouple audit emission from the parent xact (such refactors would
// silently create attestation-set audit rows for releases that never
// existed, polluting compliance evidence).
func TestReleaseStore_InsertRolledBack_AuditLogNotPersisted(t *testing.T) {
	pool := setupCrossTenantTestDB(t)
	requireRekorURLTriggers(t, pool)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantID := "tn-rekor-rb-" + suffix
	buildID := "bld-rekor-rb-" + suffix
	releaseID := "rel-rekor-rb-" + suffix
	correlationID := "corr-rekor-rb-" + suffix
	rekorURL := "https://rekor.loop-runtime.test/api/v1/log/entries?logIndex=666"

	// Same debt as the 000061 trigger: the tenant must exist before the build.
	seedTenantForBuild(t, pool, tenantID)

	// Pre-seed the build OUTSIDE the rolled-back tx so the FK can resolve.
	_, err := pool.Exec(ctx,
		`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
		 VALUES ($1, $2, 'proj-rekor', 'production', 'SUCCEEDED', $3, 'system:test')`,
		buildID, tenantID, correlationID)
	if err != nil {
		t.Fatalf("seed build (pre-tx): %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM builds WHERE id = $1", buildID) })

	// Open xact, INSERT release (trigger fires + queues audit_log row),
	// then ROLLBACK — both INSERTs evaporate.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("BEGIN: %v", err)
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by, image_attestation_rekor_url)
		 VALUES ($1, $2, $3, 'proj-rekor', 'production', 'READY', $4, 'system:test', $5)`,
		releaseID, buildID, tenantID, correlationID, &rekorURL)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("INSERT release in tx: %v", err)
	}
	// Sanity check WITHIN the tx: the audit row exists (trigger fired).
	var inTxCount int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_log
		 WHERE resource_id = $1 AND action = 'release.attestation_url_set'`,
		releaseID).Scan(&inTxCount); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("count audit_log in-tx: %v", err)
	}
	if inTxCount != 1 {
		_ = tx.Rollback(ctx)
		t.Fatalf("audit_log row should exist mid-tx (trigger fired); got %d", inTxCount)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("ROLLBACK: %v", err)
	}

	// After ROLLBACK: zero audit rows for this release.
	var postCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_log
		 WHERE resource_id = $1 AND action = 'release.attestation_url_set'`,
		releaseID).Scan(&postCount); err != nil {
		t.Fatalf("count audit_log post-rollback: %v", err)
	}
	if postCount != 0 {
		t.Errorf("audit_log row leaked across ROLLBACK boundary: got %d, want 0", postCount)
	}

	// And no orphan release.
	var releaseCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM releases WHERE id = $1`, releaseID).Scan(&releaseCount); err != nil {
		t.Fatalf("count releases post-rollback: %v", err)
	}
	if releaseCount != 0 {
		t.Errorf("release row leaked across ROLLBACK boundary: got %d, want 0", releaseCount)
	}
}
