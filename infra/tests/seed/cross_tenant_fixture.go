// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package seed — cross-tenant adversarial fixture (T1.4).
//
// Seeds TWO tenants (A and B) with parallel assets (project, build, release,
// env_target, secret, idempotency_key, audit_log entry, tenant_member rows).
// Each user is a member of EXACTLY ONE tenant. The fixture is consumed by
// cross-tenant adversarial tests that assert tenant A cannot read/modify
// tenant B's data via the API, even when:
//
//   - JWT claims tenant_id = B but user is not in tenant_members(B)
//   - Service-auth header proposes X-LoopZero-Tenant = B
//   - Idempotency-Key from a previous tenant A request is replayed under B
//
// Idempotent. Safe to re-run. Forward-compatible with later T1.9 cost_counters
// fixture.

package seed

import (
	"fmt"
	"os/exec"
)

// Cross-tenant fixture constants. UUIDs are deterministic so multiple test
// runs produce stable expectations.
const (
	// XTTenantAID is the "victim" tenant. Holds assets the test will attempt
	// to read/modify from tenant B.
	XTTenantAID = "tn-xt-tenant-a"
	// XTTenantBID is the "attacker" tenant. Issues all adversarial requests.
	XTTenantBID = "tn-xt-tenant-b"

	// XTUserAID is the user who legitimately belongs to tenant A.
	XTUserAID = "github-xt-user-a"
	// XTUserAGitHubID is the unique GitHub ID for user A.
	XTUserAGitHubID = 990001
	// XTUserAEmail is the email for user A.
	XTUserAEmail = "xt-user-a@example.com"
	// XTUserAUsername is the username for user A.
	XTUserAUsername = "xt-user-a"

	// XTUserBID is the user who legitimately belongs to tenant B.
	XTUserBID = "github-xt-user-b"
	// XTUserBGitHubID is the unique GitHub ID for user B.
	XTUserBGitHubID = 990002
	// XTUserBEmail is the email for user B.
	XTUserBEmail = "xt-user-b@example.com"
	// XTUserBUsername is the username for user B.
	XTUserBUsername = "xt-user-b"

	// XTProjectAID is the project owned by tenant A.
	XTProjectAID = "proj-xt-a"
	// XTProjectAName is the project name in tenant A.
	XTProjectAName = "xt-project-a"
	// XTProjectBID is the project owned by tenant B.
	XTProjectBID = "proj-xt-b"
	// XTProjectBName is the project name in tenant B.
	XTProjectBName = "xt-project-b"

	// XTBuildAID is build owned by tenant A.
	XTBuildAID = "bld-xt-a"
	// XTBuildBID is build owned by tenant B.
	XTBuildBID = "bld-xt-b"

	// XTReleaseAID is release owned by tenant A.
	XTReleaseAID = "rel-xt-a"
	// XTReleaseBID is release owned by tenant B.
	XTReleaseBID = "rel-xt-b"

	// XTSecretAName is the secret name in tenant A's project.
	XTSecretAName = "xt-secret-a"
	// XTSecretBName is the secret name in tenant B's project.
	XTSecretBName = "xt-secret-b"

	// XTIdempotencyKeyA is the idempotency key recorded under tenant A.
	// Used to test cross-tenant replay attacks.
	XTIdempotencyKeyA = "REDACTED_SECRET_048"

	// XTEnvProduction is the canonical environment name (both tenants use it).
	XTEnvProduction = "production"
)

// CrossTenantFixture seeds two adversarial-test tenants into the database.
// Returns nil on success; psql output is included on failure for diagnosis.
//
// The fixture writes directly via psql — NOT via API — because the API's
// auth middleware is precisely what we want to test. Seeding via SQL produces
// a deterministic adversarial state regardless of API correctness.
//
// Pre-conditions:
//   - PostgreSQL reachable at dbURL
//   - Migrations through 000016 applied (the schema and the RLS policies)
//
// Idempotent: re-running produces the same state. Cleanup uses ON CONFLICT
// DO NOTHING / DO UPDATE consistent with the BAU seed.
func SeedCrossTenantFixture(dbURL string) error {
	sql := fmt.Sprintf(`
-- T1.4 cross-tenant adversarial fixture.
--
-- Seeds two tenants and their assets. Each user is a member of EXACTLY ONE
-- tenant — that is the adversarial premise. The RLS policies at the DB
-- layer + the auth middleware membership check at the API layer must reject
-- every cross-tenant request.
--
-- Defensive: ensure cell-default exists in cells before tenant INSERT.
INSERT INTO cells (cell_id, display_name, status, region)
VALUES ('cell-default', 'Default cell (local)', 'active', 'local')
ON CONFLICT (cell_id) DO NOTHING;

-- Users (one per tenant).
INSERT INTO users (id, email, github_id, display_name, username)
VALUES
  ('%s', '%s', %d, 'XT User A', '%s'),
  ('%s', '%s', %d, 'XT User B', '%s')
ON CONFLICT (id) DO NOTHING;

-- Tenants (one per user, distinct cells handled by DEFAULT).
INSERT INTO tenants (id, display_name, slug, owner_id, cell_id)
VALUES
  ('%s', 'XT Tenant A', '%s', '%s', 'cell-default'),
  ('%s', 'XT Tenant B', '%s', '%s', 'cell-default')
ON CONFLICT (id) DO UPDATE SET cell_id = COALESCE(tenants.cell_id, EXCLUDED.cell_id);

-- Memberships: each user is a member of EXACTLY ONE tenant. NO crossover.
INSERT INTO tenant_members (tenant_id, user_id, role)
VALUES
  ('%s', '%s', 'owner'),
  ('%s', '%s', 'owner')
ON CONFLICT (tenant_id, user_id) DO NOTHING;

-- Projects (one per tenant).
INSERT INTO projects (id, tenant_id, name, created_by)
VALUES
  ('%s', '%s', '%s', '%s'),
  ('%s', '%s', '%s', '%s')
ON CONFLICT (id) DO NOTHING;

-- Builds (one per tenant). Status QUEUED so the row exists but is not
-- terminal — tests can attempt read/cancel/promote.
INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
VALUES
  ('%s', '%s', '%s', '%s', 'QUEUED', 'corr-xt-a', '%s'),
  ('%s', '%s', '%s', '%s', 'QUEUED', 'corr-xt-b', '%s')
ON CONFLICT (id) DO NOTHING;

-- Releases (one per tenant). State PENDING so promote/rollback flows can be
-- attempted cross-tenant.
INSERT INTO releases (id, tenant_id, project_id, build_id, environment, status, image_ref, created_at)
VALUES
  ('%s', '%s', '%s', '%s', '%s', 'PENDING', 'registry.example/xt-a:abc', now()),
  ('%s', '%s', '%s', '%s', '%s', 'PENDING', 'registry.example/xt-b:def', now())
ON CONFLICT (id) DO NOTHING;

-- Environment targets (one per tenant) — operational pointer that
-- promote/rollback updates.
INSERT INTO environment_targets (tenant_id, project_id, environment, current_release_id, updated_at)
VALUES
  ('%s', '%s', '%s', NULL, now()),
  ('%s', '%s', '%s', NULL, now())
ON CONFLICT (tenant_id, project_id, environment) DO NOTHING;

-- Secrets (one per tenant) — high-value adversarial target.
-- The value column stores the encrypted blob; here we use a placeholder
-- so the test can prove A's secret value is NEVER returned to B.
INSERT INTO secrets (id, tenant_id, project_id, environment, name, value_ciphertext, created_at)
VALUES
  ('sec-xt-a', '%s', '%s', '%s', '%s', 'ENCRYPTED-A-DO-NOT-LEAK', now()),
  ('sec-xt-b', '%s', '%s', '%s', '%s', 'ENCRYPTED-B-DO-NOT-LEAK', now())
ON CONFLICT (id) DO NOTHING;

-- Idempotency key recorded under tenant A. Tests will attempt to replay
-- this key as tenant B and assert the API returns a fresh 4xx (NOT a
-- replay of tenant A's cached response).
INSERT INTO idempotency_keys (tenant_id, key, request_hash, response_status, response_body, created_at, expires_at)
VALUES
  ('%s', '%s', 'hash-xt-a', 202, '{"build_id":"%s"}', now(), now() + interval '24 hours')
ON CONFLICT (tenant_id, key) DO NOTHING;

-- Audit log entry under tenant A. Tests will attempt to read this from
-- tenant B and assert RLS returns zero rows (not the tenant A entry).
INSERT INTO audit_log (id, tenant_id, user_id, action, resource_type, resource_id, metadata, created_at)
VALUES
  ('audit-xt-a-1', '%s', '%s', 'build.submitted', 'build', '%s', '{"correlation_id":"corr-xt-a"}'::jsonb, now())
ON CONFLICT (id) DO NOTHING;
`,
		// Users
		XTUserAID, XTUserAEmail, XTUserAGitHubID, XTUserAUsername,
		XTUserBID, XTUserBEmail, XTUserBGitHubID, XTUserBUsername,
		// Tenants (slug = tenant id for stability)
		XTTenantAID, XTTenantAID, XTUserAID,
		XTTenantBID, XTTenantBID, XTUserBID,
		// Memberships
		XTTenantAID, XTUserAID,
		XTTenantBID, XTUserBID,
		// Projects
		XTProjectAID, XTTenantAID, XTProjectAName, XTUserAID,
		XTProjectBID, XTTenantBID, XTProjectBName, XTUserBID,
		// Builds
		XTBuildAID, XTTenantAID, XTProjectAName, XTEnvProduction, XTUserAID,
		XTBuildBID, XTTenantBID, XTProjectBName, XTEnvProduction, XTUserBID,
		// Releases (project_id is the project NAME per LoopZero convention)
		XTReleaseAID, XTTenantAID, XTProjectAName, XTBuildAID, XTEnvProduction,
		XTReleaseBID, XTTenantBID, XTProjectBName, XTBuildBID, XTEnvProduction,
		// Environment targets
		XTTenantAID, XTProjectAName, XTEnvProduction,
		XTTenantBID, XTProjectBName, XTEnvProduction,
		// Secrets
		XTTenantAID, XTProjectAName, XTEnvProduction, XTSecretAName,
		XTTenantBID, XTProjectBName, XTEnvProduction, XTSecretBName,
		// Idempotency
		XTTenantAID, XTIdempotencyKeyA, XTBuildAID,
		// Audit
		XTTenantAID, XTUserAID, XTBuildAID,
	)

	cmd := exec.Command("psql", dbURL, "-v", "ON_ERROR_STOP=1", "-c", sql)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("psql failed: %w\noutput: %s", err, output)
	}
	return nil
}

// CleanCrossTenantFixture removes all rows seeded by SeedCrossTenantFixture.
// Idempotent. Used by tests that want a clean slate.
//
// Order matters: dependent rows are deleted before parents to satisfy FK
// constraints.
func CleanCrossTenantFixture(dbURL string) error {
	sql := fmt.Sprintf(`
DELETE FROM audit_log WHERE tenant_id IN ('%s', '%s');
DELETE FROM idempotency_keys WHERE tenant_id IN ('%s', '%s');
DELETE FROM secrets WHERE tenant_id IN ('%s', '%s');
DELETE FROM environment_targets WHERE tenant_id IN ('%s', '%s');
DELETE FROM releases WHERE tenant_id IN ('%s', '%s');
DELETE FROM builds WHERE tenant_id IN ('%s', '%s');
DELETE FROM projects WHERE tenant_id IN ('%s', '%s');
DELETE FROM tenant_members WHERE tenant_id IN ('%s', '%s');
DELETE FROM tenants WHERE id IN ('%s', '%s');
DELETE FROM users WHERE id IN ('%s', '%s');
`,
		XTTenantAID, XTTenantBID,
		XTTenantAID, XTTenantBID,
		XTTenantAID, XTTenantBID,
		XTTenantAID, XTTenantBID,
		XTTenantAID, XTTenantBID,
		XTTenantAID, XTTenantBID,
		XTTenantAID, XTTenantBID,
		XTTenantAID, XTTenantBID,
		XTTenantAID, XTTenantBID,
		XTUserAID, XTUserBID,
	)
	cmd := exec.Command("psql", dbURL, "-v", "ON_ERROR_STOP=1", "-c", sql)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("psql cleanup failed: %w\noutput: %s", err, output)
	}
	return nil
}
