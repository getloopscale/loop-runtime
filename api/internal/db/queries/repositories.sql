-- queries/repositories.sql — sqlc query definitions for the
-- project_repositories + github_installations tables.
--
-- T2.1 (Batch A #3) of plan docs/plans/2026-05-29-sqlc-migration-plan.md
-- v1.3. Backs `loopzero connect` (project ↔ GitHub repo linkage) and the
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
-- sometimes lacks the full payload). tenant_id is COALESCED so a
-- prior tenant binding survives a webhook with NULL tenant.
INSERT INTO github_installations (
  id, account_login, account_type, tenant_id, created_at, updated_at
) VALUES (
  $1, $2, $3, $4, NOW(), NOW()
)
ON CONFLICT (id) DO UPDATE SET
  account_login = CASE WHEN EXCLUDED.account_login = '' THEN github_installations.account_login ELSE EXCLUDED.account_login END,
  tenant_id = COALESCE(EXCLUDED.tenant_id, github_installations.tenant_id),
  updated_at = NOW();

-- tenantpredicate:exempt
-- name: GetGitHubInstallation :one
-- Lookup by installation id (GitHub-side principal). Cross-tenant
-- because the installation itself decides which tenant binding to
-- surface.
SELECT id, account_login, account_type, tenant_id, created_at, updated_at
FROM github_installations
WHERE id = $1;

-- tenantpredicate:exempt
-- name: DeleteGitHubInstallation :exec
-- FK cascade deletes project_repositories rows owned by this
-- installation. Webhook-driven; no tenant context.
DELETE FROM github_installations WHERE id = $1;

-- =========================================================================
-- project_repositories (project ↔ GitHub repo link)
-- =========================================================================

-- name: LinkProjectRepository :exec
-- Idempotent re-link per `loopzero connect` CLI contract: ON CONFLICT
-- (repo_full_name) DO UPDATE refreshes tenant/project/installation/
-- branch fields and bumps updated_at. The pre-INSERT lookup in the
-- route handler still rejects cross-tenant or cross-project re-links;
-- this only handles the legitimate "same caller re-connects" path.
INSERT INTO project_repositories (
  id, tenant_id, project_name, provider, repo_full_name,
  installation_id, default_branch, auto_deploy, created_at, updated_at
) VALUES (
  $1, $2, $3, 'github', $4, $5, $6, true, NOW(), NOW()
)
ON CONFLICT (repo_full_name) DO UPDATE SET
  tenant_id = EXCLUDED.tenant_id,
  project_name = EXCLUDED.project_name,
  installation_id = EXCLUDED.installation_id,
  default_branch = EXCLUDED.default_branch,
  auto_deploy = true,
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
