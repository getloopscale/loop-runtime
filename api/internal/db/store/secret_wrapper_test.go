// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

// secret_wrapper_test.go — T7.1 of plan 2026-06-01-p3-infra-observability-
// test-debt-plan.md v1.1 — regression tests for V9 L19 fix (secret PK
// includes environment). Memory `reference_v9_session_complete_2026_05_30`
// records L19 was caught LIVE via dogfood, never had a unit test.
//
// L19 bug recap: pre-fix PK was `sec-<tenant>-<project>-<app>-<name>` →
// the same (tenant, project, app, name) tuple across DIFFERENT envs
// collided on the PK before the ON CONFLICT clause (which compares the
// unique-constraint columns, not the PK) could fire. v9L16 dogfood
// surfaced this when Journey 03 set "j-secrets/secret-app" env after a
// prior journey had set "production/secret-app" — the second INSERT
// failed with SQLSTATE 23505 ("duplicate key violates secrets_pkey")
// and returned HTTP 500 INTERNAL_ERROR.
//
// Fix shipped in secret_wrapper.go:145 — `sec-<tenant>-<project>-<env>-
// <app>-<name>` — the PK is now derivable from the SAME tuple as the
// unique constraint, so distinct envs produce distinct PKs.
//
// These tests prove BOTH directions of the invariant:
//   1. Same (tenant, project, app, name) across 4 envs → 4 distinct PKs,
//      all rows INSERT successfully (TestSecret_PKIncludesEnvironment).
//   2. Duplicate (tenant, project, env, app, name) → second call is an
//      UPDATE (UPSERT), not an INSERT collision
//      (TestSecret_UpsertOnDuplicateEnvironment).
//
// Per D6 ADR of the plan, this is an integration test against a real
// PostgreSQL instance via DATABASE_URL (default
// postgres://postgres:postgres@localhost:5432/loop_runtime_test). Skips
// gracefully when PG is unreachable so the wider unit suite is not
// blocked by infra availability.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// secretPerEnvironmentPool opens a pool or skips the test if PG is unreachable. The
// pool is closed via t.Cleanup.
func secretPerEnvironmentPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, fixtureDBURL())
	if err != nil {
		t.Skipf("L19 test skipped: cannot create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("L19 test skipped: cannot ping PG: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// requireSecretsTable skips the test when the secrets table is absent
// (DB not migrated). Same idiomatic skip used by requireRekorURLTriggers
// in api/tests/integration/release_attestation_url_test.go.
func requireSecretsTable(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'secrets'
		)`).Scan(&exists)
	if err != nil {
		t.Skipf("L19 test skipped: cannot query information_schema: %v", err)
	}
	if !exists {
		t.Skipf("L19 test skipped: secrets table missing (run `task db:migrate`)")
	}
}

// TestSecret_PKIncludesEnvironment (T7.1 RED #1): four envs for the same
// (tenant, project, app, name) tuple all INSERT successfully because
// the PK template now includes environment. Pre-L19 the second call
// would fail with secrets_pkey violation.
func TestSecret_PKIncludesEnvironment(t *testing.T) {
	pool := secretPerEnvironmentPool(t)
	requireSecretsTable(t, pool)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantID := "tn-l19-" + suffix
	projectID := "proj-l19"
	appName := "app-l19"
	secretName := "db-password"
	// Mix of envs including the historical regression case "j-secrets"
	// that LIVE-caught L19 in v9L16 dogfood.
	envs := []string{"production", "staging", "j-secrets", "preview"}

	// Cleanup: delete all rows for this tenant suffix at the end.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			"DELETE FROM secrets WHERE tenant_id = $1", tenantID)
	})

	store, err := NewPostgresSecretStoreWithVault(&db.PgxAdapter{Pool: pool}, echoTransit{}, nil)
	if err != nil {
		t.Fatalf("NewPostgresSecretStore: %v", err)
	}

	// INSERT each env; all four MUST succeed and report created=true.
	for _, env := range envs {
		created, err := store.Set(ctx, tenantID, projectID, env, appName, secretName,
			[]byte("value-for-"+env))
		if err != nil {
			t.Fatalf("Set(env=%q) failed (L19 regression): %v", env, err)
		}
		if !created {
			t.Errorf("Set(env=%q) expected created=true on first INSERT, got false", env)
		}
	}

	// Assert all 4 rows exist with distinct PKs.
	rows, err := pool.Query(ctx,
		`SELECT id, environment FROM secrets
		 WHERE tenant_id = $1 AND project_id = $2 AND app = $3 AND name = $4
		 ORDER BY environment`,
		tenantID, projectID, appName, secretName)
	if err != nil {
		t.Fatalf("SELECT secrets: %v", err)
	}
	defer rows.Close()

	ids := map[string]string{}
	for rows.Next() {
		var id, env string
		if err := rows.Scan(&id, &env); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids[env] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	if len(ids) != len(envs) {
		t.Fatalf("expected %d rows (one per env), got %d: %v", len(envs), len(ids), ids)
	}
	// Each PK must contain its env so we can prove the fix produced
	// the documented "sec-<tenant>-<project>-<env>-<app>-<name>" shape.
	for env, id := range ids {
		want := fmt.Sprintf("sec-%s-%s-%s-%s-%s", tenantID, projectID, env, appName, secretName)
		if id != want {
			t.Errorf("env=%q PK mismatch: got %q, want %q (L19 PK template drift)", env, id, want)
		}
	}

	// Also assert all PKs are pairwise distinct (defense-in-depth — if
	// the template ever drops env again, this catches it).
	seen := map[string]string{}
	for env, id := range ids {
		if prev, dup := seen[id]; dup {
			t.Errorf("PK collision across envs %q and %q: both = %q (L19 regression)", env, prev, id)
		}
		seen[id] = env
	}
}

// TestSecret_UpsertOnDuplicateEnvironment (T7.1 RED #2): a second Set with the
// SAME (tenant, project, env, app, name) tuple is an UPSERT — created=
// false on the second call (UPDATE path), value is replaced. This proves
// the unique constraint + ON CONFLICT path still works after the PK
// change, not just the new-row path.
//
// This is the negative-companion of TestSecret_PKIncludesEnvironment: it
// closes the symmetric case where the PK SHOULD collide (same env) and
// be handled gracefully via UPSERT, NOT bubble up as SQLSTATE 23505.
func TestSecret_UpsertOnDuplicateEnvironment(t *testing.T) {
	pool := secretPerEnvironmentPool(t)
	requireSecretsTable(t, pool)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantID := "tn-l19up-" + suffix
	projectID := "proj-l19up"
	appName := "app-l19up"
	secretName := "api-token"
	env := "production"

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			"DELETE FROM secrets WHERE tenant_id = $1", tenantID)
	})

	store, err := NewPostgresSecretStoreWithVault(&db.PgxAdapter{Pool: pool}, echoTransit{}, nil)
	if err != nil {
		t.Fatalf("NewPostgresSecretStore: %v", err)
	}

	// First Set: created=true (INSERT path).
	created1, err := store.Set(ctx, tenantID, projectID, env, appName, secretName, []byte("v1"))
	if err != nil {
		t.Fatalf("first Set: %v", err)
	}
	if !created1 {
		t.Error("expected created=true on first Set")
	}

	// Second Set with same tuple: created=false (UPDATE path); the
	// pre-L19 PK would have raised secrets_pkey violation here BEFORE
	// the ON CONFLICT clause could fire. Post-L19 the UPSERT works.
	created2, err := store.Set(ctx, tenantID, projectID, env, appName, secretName, []byte("v2"))
	if err != nil {
		t.Fatalf("second Set (UPSERT): %v", err)
	}
	if created2 {
		t.Error("expected created=false on second Set (UPDATE path)")
	}

	// Verify only ONE row exists (not two) and its value reflects v2.
	var count int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM secrets
		 WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
		 AND app = $4 AND name = $5`,
		tenantID, projectID, env, appName, secretName).Scan(&count)
	if err != nil {
		t.Fatalf("COUNT: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 row after UPSERT, got %d (UPSERT may have INSERTed instead)", count)
	}

	var stored []byte
	err = pool.QueryRow(ctx,
		`SELECT encrypted_value FROM secrets
		 WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
		 AND app = $4 AND name = $5`,
		tenantID, projectID, env, appName, secretName).Scan(&stored)
	if err != nil {
		t.Fatalf("SELECT value: %v", err)
	}
	if want := echoCiphertext("v2"); string(stored) != want {
		t.Errorf("expected stored value=%q after UPDATE, got %q", want, string(stored))
	}
}

// echoTransit is a transit client whose ciphertext names its plaintext, so a test can
// tell which value the row holds. The store refuses to write without a transit client.
type echoTransit struct{}

func echoCiphertext(plaintext string) string { return "vault:v1:echo:" + plaintext }

func (echoTransit) Encrypt(_ context.Context, _ string, plaintext []byte) (string, error) {
	return echoCiphertext(string(plaintext)), nil
}

func (echoTransit) Decrypt(_ context.Context, _ string, ciphertext string) ([]byte, error) {
	return []byte(strings.TrimPrefix(ciphertext, echoCiphertext(""))), nil
}
