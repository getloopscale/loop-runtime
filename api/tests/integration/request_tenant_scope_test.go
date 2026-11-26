// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	loopruntimedb "github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/dbtx"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
)

// The request pool runs as loopruntime_api, and every tenant table is under
// fail-closed Row Level Security (migration 000062): a statement sees only the
// rows of the tenant named by `app.tenant_id`. Nothing on the request path set
// that variable outside the environment lock, so every read of a tenant table
// on an authenticated request saw nothing — measured on 2026-09-25 against the
// migrated schema: a user's own membership row read 0 rows as loopruntime_api and 1 as
// loopruntime_worker.
//
// The request pool now scopes every connection it hands out to the tenant the
// request context carries. These tests pin that behaviour over the real role:
// the scoped tenant's rows are visible, another tenant's are not, and a
// connection reused by an unscoped caller carries no tenant forward.
//
//	DATABASE_URL=<loopruntime_api> LOOPRUNTIME_WORKER_DATABASE_URL=<loopruntime_worker> \
//	  go test ./tests/integration/ -run RequestPool -v

const requestScopeTenantA, requestScopeTenantB = "tn-rls-scope-a", "tn-rls-scope-b"

// requestScopeSeed writes one project per tenant through the cross-tenant
// role, so the fixture does not depend on the mechanism under test.
func requestScopeSeed(t *testing.T) {
	t.Helper()
	const owner, acct = "rls-scope-uid", "acct-rls-scope"
	_ = testPool(t) // skips when PostgreSQL is unreachable
	seed, err := sql.Open("pgx", seedDBURL())
	if err != nil {
		t.Fatalf("open seed DB: %v", err)
	}
	t.Cleanup(func() { _ = seed.Close() })
	projectCapSeed(t, seed, owner, acct, requestScopeTenantA, requestScopeTenantB, "starter")
	t.Cleanup(func() { projectCapCleanup(t, seed, owner) })
	for _, tn := range []string{requestScopeTenantA, requestScopeTenantB} {
		if _, err := seed.Exec(`INSERT INTO projects (id, tenant_id, name, created_by) VALUES ($1, $2, 'p', $3)`,
			"prj-"+tn, tn, owner); err != nil {
			t.Fatalf("seed project for %s: %v", tn, err)
		}
	}
}

// scopedRequestPool builds the request pool the way setupDatabase does, over the
// request role, with a single connection so every call reuses it.
func scopedRequestPool(t *testing.T) *loopruntimedb.PgxAdapter {
	t.Helper()
	requireIsolatedRequestRole(t)
	return tenantScopedRequestPool(t, 1)
}

// tenantScopedRequestPool builds the pool that serves HTTP requests the way
// setupDatabase does (requestPoolConfig): over the request role, every
// connection scoped to the tenant its context carries (dbtx.WithTenantScope). Code under test that
// production runs on the request path runs on this pool, not on a raw one: a
// raw connection carries whatever `app.tenant_id` a previous user left on it,
// which is how a fixture's scope leaked into the code under test and made
// TestConcurrentBuildSubmission pass or fail by connection (issue #387).
func tenantScopedRequestPool(t *testing.T, maxConns int32) *loopruntimedb.PgxAdapter {
	t.Helper()
	_ = testPool(t) // skips when PostgreSQL is unreachable
	adapter, err := loopruntimedb.NewPool(context.Background(), dbURL(), loopruntimedb.PoolConfig{
		MaxConns:          maxConns,
		HealthCheckPeriod: time.Minute,
		MaxConnLifetime:   time.Hour,
		MaxConnIdleTime:   time.Hour,
		ConnectTimeout:    10 * time.Second,
		StatementTimeout:  "30000",
		TenantScoped:      true,
	})
	if err != nil {
		t.Fatalf("open request pool: %v", err)
	}
	t.Cleanup(adapter.Close)
	return adapter
}

// requireIsolatedRequestRole skips when DATABASE_URL can bypass RLS: over such
// a role every assertion below would pass for the wrong reason.
func requireIsolatedRequestRole(t *testing.T) {
	t.Helper()
	pool := testPool(t)
	var bypasses bool
	if err := pool.QueryRow(context.Background(),
		"SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user").Scan(&bypasses); err != nil {
		t.Fatalf("read the request role's RLS posture: %v", err)
	}
	if bypasses {
		// A role that bypasses RLS cannot reproduce https://github.com/getloopscale/loop-runtime/issues/384:
		// it sees every tenant's rows whether or not the connection is scoped.
		t.Skip("DATABASE_URL is not the isolated request role (it bypasses RLS); point it at loopruntime_api")
	}
}

func projectsVisible(t *testing.T, ctx context.Context, adapter *loopruntimedb.PgxAdapter) []string {
	t.Helper()
	rows, err := adapter.Query(ctx, `SELECT tenant_id FROM projects WHERE tenant_id IN ($1, $2) ORDER BY tenant_id`,
		requestScopeTenantA, requestScopeTenantB)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	defer rows.Close()
	var tenants []string
	for rows.Next() {
		var tn string
		if err := rows.Scan(&tn); err != nil {
			t.Fatalf("scan: %v", err)
		}
		tenants = append(tenants, tn)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	return tenants
}

func TestRequestPoolShowsTheScopedTenantItsOwnRows(t *testing.T) {
	requestScopeSeed(t)
	adapter := scopedRequestPool(t)

	got := projectsVisible(t, dbtx.WithTenantScope(context.Background(), requestScopeTenantA), adapter)

	if len(got) != 1 || got[0] != requestScopeTenantA {
		t.Fatalf("projects visible to %s = %v; want exactly its own", requestScopeTenantA, got)
	}
}

func TestRequestPoolSwitchesTheScopeWhenTheConnectionIsReusedByAnotherTenant(t *testing.T) {
	requestScopeSeed(t)
	adapter := scopedRequestPool(t)
	_ = projectsVisible(t, dbtx.WithTenantScope(context.Background(), requestScopeTenantA), adapter)

	got := projectsVisible(t, dbtx.WithTenantScope(context.Background(), requestScopeTenantB), adapter)

	if len(got) != 1 || got[0] != requestScopeTenantB {
		t.Fatalf("projects visible to %s on a reused connection = %v; want exactly its own", requestScopeTenantB, got)
	}
}

func TestRequestPoolCarriesNoTenantIntoAnUnscopedCaller(t *testing.T) {
	requestScopeSeed(t)
	adapter := scopedRequestPool(t)
	_ = projectsVisible(t, dbtx.WithTenantScope(context.Background(), requestScopeTenantA), adapter)

	got := projectsVisible(t, context.Background(), adapter)

	if len(got) != 0 {
		t.Fatalf("an unscoped caller on a connection last used by %s saw %v; want nothing", requestScopeTenantA, got)
	}
}

// A rejected authentication belongs to no tenant: the auditor writes it with
// an empty tenant_id before any tenant is known. On the request role that row is
// admitted only when the connection's scope is the empty string, and a fresh
// session reads NULL instead — so the rejection was refused and the security
// trail lost, with nothing but a log line.
func TestRequestPoolRecordsARejectedAuthenticationWithNoTenant(t *testing.T) {
	adapter := scopedRequestPool(t)
	// audit_log is append-only, so the probe row stays behind.
	const action = "auth.rls_scope_probe"

	err := store.WriteAuditLog(context.Background(), adapter, store.AuditEntry{
		Action: action, Actor: "anonymous", ResourceType: "auth", ResourceID: "probe",
	})

	if err != nil {
		t.Fatalf("pre-authentication audit write over the request pool: %v", err)
	}
}
