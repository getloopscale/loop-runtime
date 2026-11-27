-- queries/repositories.sql — sqlc query definitions for the
-- project_repositories + github_installations tables.
--
-- T2.1 (Batch A #3) of plan 2026-05-29-sqlc-migration-plan
-- v1.3. Backs `loop_runtime connect` (project ↔ GitHub repo linkage) and the
-- GitHub App webhook → installation lifecycle.
--
-- Tenant predicate model:
--   - project_repositories is in TenantOwnedTables (T2.1 #3 extension).
--     LinkRepository / UnlinkRepository / GetByProject ALL carry
--     `tenant_id = $N` (the multi-tenant API path). GetByRepo is
--     marked tenantpredicate:exempt — it's the WEBHOOK path where
--     GitHub delivers events keyed by repo_full_name with NO tenant
--     context; the lookup MUST cross tenants by design (the row
--     itself surfaces the owning tenant_id for downstream
--     authorization).
--   - github_installations is NOT in TenantOwnedTables — it's a
--     GitHub-side principal record (one row per GitHub App
--     installation; tenant_id is a NULLABLE soft-mapping populated
--     when the operator connects an installation to a tenant). All
--     queries here are by installation id, so they're marked
--     exempt.

-- =========================================================================
-- github_installations (GitHub App installation lifecycle)
-- =========================================================================

-- tenantpredicate:exempt
-- name: UpsertGitHubInstallation :exec
-- Idempotent upsert: ON CONFLICT (id) DO UPDATE preserves the
-- existing account_login when the incoming value is empty (webhook
-- sometimes lacks the full payload). An existing tenant binding always
-- survives: this statement fills tenant_id only on a row that has none, and
-- never sets verified_at. Binding an installation to a tenant is
-- ClaimGitHubInstallation's job.
INSERT INTO github_installations (
  id, account_login, account_type, tenant_id, created_at, updated_at
) VALUES (
  $1, $2, $3, $4, NOW(), NOW()
)
ON CONFLICT (id) DO UPDATE SET
  account_login = CASE WHEN EXCLUDED.account_login = '' THEN github_installations.account_login ELSE EXCLUDED.account_login END,
  tenant_id = COALESCE(github_installations.tenant_id, EXCLUDED.tenant_id),
  updated_at = NOW();

-- tenantpredicate:exempt
-- name: GetGitHubInstallation :one
-- Lookup by installation id (GitHub-side principal). Cross-tenant
-- because the installation itself decides which tenant binding to
-- surface.
SELECT id, account_login, account_type, tenant_id, created_at, updated_at,
       verified_at, verified_by
FROM github_installations
WHERE id = $1;

-- tenantpredicate:exempt
-- name: ClaimGitHubInstallation :one
-- Binds an installation to a tenant after GitHub confirmed that the user
-- (verified_by) can access it, and records when. One statement, so two
-- tenants claiming the same installation cannot both win: the row is written
-- when the installation is unbound or already belongs to this tenant, and
-- left alone, returning no row, when it belongs to another one.
INSERT INTO github_installations (
  id, account_login, account_type, tenant_id, verified_at, verified_by,
  created_at, updated_at
) VALUES (
  $1, $2, $3, $4, NOW(), $5, NOW(), NOW()
)
ON CONFLICT (id) DO UPDATE SET
  account_login = CASE WHEN EXCLUDED.account_login = '' THEN github_installations.account_login ELSE EXCLUDED.account_login END,
  account_type = EXCLUDED.account_type,
  tenant_id = EXCLUDED.tenant_id,
  verified_at = NOW(),
  verified_by = EXCLUDED.verified_by,
  updated_at = NOW()
WHERE github_installations.tenant_id IS NULL
   OR github_installations.tenant_id = EXCLUDED.tenant_id
RETURNING id;

-- name: ListVerifiedGitHubInstallationsByTenant :many
-- The installations a tenant may use: bound to it, with the binding confirmed
-- on GitHub.
SELECT id, account_login, account_type, tenant_id, created_at, updated_at,
       verified_at, verified_by
FROM github_installations
WHERE tenant_id = $1
  AND verified_at IS NOT NULL
ORDER BY id;

-- tenantpredicate:exempt
-- name: DeleteGitHubInstallation :exec
-- FK cascade deletes project_repositories rows owned by this
-- installation. Webhook-driven; no tenant context.
DELETE FROM github_installations WHERE id = $1;

-- =========================================================================
-- project_repositories (project ↔ GitHub repo link)
-- =========================================================================

-- name: LinkProjectRepository :exec
-- Idempotent re-link per `loop_runtime connect` CLI contract: ON CONFLICT
-- (repo_full_name) DO UPDATE refreshes tenant/project/installation/
-- branch fields and bumps updated_at. The pre-INSERT lookup in the
-- route handler still rejects cross-tenant or cross-project re-links;
-- this only handles the legitimate "same caller re-connects" path.
--
-- Plan J-09 T3.2 (2026-06-05): auto_deploy passed as $7 (was hardcoded
-- to true). ON CONFLICT respects EXCLUDED so users can pause auto-deploy
-- without unlink/relink.
INSERT INTO project_repositories (
  id, tenant_id, project_name, provider, repo_full_name,
  installation_id, default_branch, auto_deploy, created_at, updated_at
) VALUES (
  $1, $2, $3, 'github', $4, $5, $6, $7, NOW(), NOW()
)
ON CONFLICT (repo_full_name) DO UPDATE SET
  tenant_id = EXCLUDED.tenant_id,
  project_name = EXCLUDED.project_name,
  installation_id = EXCLUDED.installation_id,
  default_branch = EXCLUDED.default_branch,
  auto_deploy = EXCLUDED.auto_deploy,
  updated_at = NOW();

-- name: UnlinkProjectRepository :execrows
-- Returns rows-affected so the wrapper can map 0 → ErrRepositoryNotLinked
-- (CHG-2026-04-23-AUDIT-21 — replaces silent 200-on-noop with a typed
-- "not linked" sentinel so the HTTP layer can return 404).
DELETE FROM project_repositories
WHERE tenant_id = $1 AND project_name = $2;

-- tenantpredicate:exempt
-- name: GetProjectRepositoryByRepo :one
-- WEBHOOK PATH: GitHub delivers events keyed by repo_full_name with
-- NO tenant context. The lookup MUST cross tenants by design — the
-- returned row carries the owning tenant_id that the downstream
-- authorization gate reads.
SELECT id, tenant_id, project_name, provider, repo_full_name, repo_id,
       installation_id, default_branch, auto_deploy, created_at, updated_at
FROM project_repositories
WHERE repo_full_name = $1;

-- name: GetProjectRepositoryByProject :one
-- API PATH: tenant-scoped lookup for the (tenant, project) tuple.
SELECT id, tenant_id, project_name, provider, repo_full_name, repo_id,
       installation_id, default_branch, auto_deploy, created_at, updated_at
FROM project_repositories
WHERE tenant_id = $1 AND project_name = $2;
