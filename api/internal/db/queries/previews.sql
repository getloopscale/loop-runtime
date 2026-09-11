-- queries/previews.sql — sqlc query definitions for the
-- preview_environments table.
--
-- T2.2 (Batch B #1) of plan 2026-05-29-sqlc-migration-plan
-- v1.3. Multi-tenant — every list/lookup query carries tenant_id
-- predicate. Single-row mutations keyed by globally-unique preview_id
-- (PRIMARY KEY) are marked tenantpredicate:exempt — the route handler
-- authenticates tenant ownership upstream via GetByID/GetByPR gates.
--
-- preview_environments has a partial UNIQUE index on
-- (tenant_id, project_id, pr_number) WHERE deleted_at IS NULL.
-- The Create query uses ON CONFLICT against that partial index so a
-- `synchronize` webhook on a PR with an existing live preview reuses
-- the same row (commit_sha + branch + url + build refresh).
--
-- Status values mirror the state machine in pkg/statemachine/preview.go
-- (creating, active, expiring, deleting, deleted, failed, cleanup_failed).

-- name: InsertPreview :one
-- UPSERT: on partial unique index (tenant_id, project_id, pr_number)
-- WHERE deleted_at IS NULL, RESET status to 'creating', CLEAR build_id
-- and release_id, REPLACE branch/commit_sha/url/repo_full_name/
-- installation_id/expires_at. Returns id + status + created_at +
-- updated_at + expires_at so the caller can stash refreshed timestamps
-- after upsert.
INSERT INTO preview_environments (
	id, tenant_id, project_id, pr_number, branch, commit_sha, status,
	url, build_id, release_id, repo_full_name, installation_id, expires_at
) VALUES (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
ON CONFLICT (tenant_id, project_id, pr_number) WHERE deleted_at IS NULL DO UPDATE SET
	branch          = EXCLUDED.branch,
	commit_sha      = EXCLUDED.commit_sha,
	status          = 'creating',
	build_id        = NULL,
	release_id      = NULL,
	url             = EXCLUDED.url,
	repo_full_name  = EXCLUDED.repo_full_name,
	installation_id = EXCLUDED.installation_id,
	expires_at      = EXCLUDED.expires_at,
	updated_at      = now()
RETURNING id, status, created_at, updated_at, expires_at;

-- tenantpredicate:exempt — keyed by preview_id (globally-unique PK).
-- name: UpdatePreviewStatus :execrows
-- Sets status; caller is responsible for state-machine validity
-- (thin DB-level setter). Wrapper maps 0 rows-affected →
-- ErrPreviewNotFound.
UPDATE preview_environments
   SET status = $1
 WHERE id = $2 AND deleted_at IS NULL;

-- tenantpredicate:exempt — keyed by preview_id (globally-unique PK).
-- name: UpdatePreviewBuild :execrows
-- Associates a build (and optionally refreshes commit_sha).
-- COALESCE(NULLIF($2, ''), commit_sha) preserves the existing commit
-- when caller passes empty string.
UPDATE preview_environments
   SET build_id   = $1,
       commit_sha = COALESCE(NULLIF($2, ''), commit_sha)
 WHERE id = $3 AND deleted_at IS NULL;

-- tenantpredicate:exempt — keyed by preview_id (globally-unique PK).
-- name: UpdatePreviewURL :execrows
-- Sets the public preview URL exposed to the user.
UPDATE preview_environments
   SET url = $1
 WHERE id = $2 AND deleted_at IS NULL;

-- name: RequestPreviewDeletion :execrows
-- Soft-delete: status='deleting' + deleted_at=now(). Row remains
-- visible for audit/history but is excluded from active uniqueness
-- (partial unique index WHERE deleted_at IS NULL). tenantID scopes
-- the operation to prevent cross-tenant deletes when callers bypass
-- RLS.
UPDATE preview_environments
   SET status     = 'deleting',
       deleted_at = now()
 WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL;

-- name: ListPreviewsByTenant :many
-- Returns all live (deleted_at IS NULL) previews for a tenant,
-- newest first.
SELECT id, tenant_id, project_id, pr_number, branch, commit_sha, status,
       url, build_id, release_id, repo_full_name, installation_id,
       expires_at, cell_id, created_at, updated_at, deleted_at
FROM preview_environments
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: ListPreviewsByProject :many
-- Tenant + project scoped listing.
SELECT id, tenant_id, project_id, pr_number, branch, commit_sha, status,
       url, build_id, release_id, repo_full_name, installation_id,
       expires_at, cell_id, created_at, updated_at, deleted_at
FROM preview_environments
WHERE tenant_id = $1 AND project_id = $2 AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: GetPreviewByIDForTenant :one
-- Tenant-scoped lookup by preview_id. Wrapper maps pgx.ErrNoRows →
-- ErrPreviewNotFound per legacy contract.
SELECT id, tenant_id, project_id, pr_number, branch, commit_sha, status,
       url, build_id, release_id, repo_full_name, installation_id,
       expires_at, cell_id, created_at, updated_at, deleted_at
FROM preview_environments
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL;

-- name: GetPreviewByPR :one
-- Tenant + project + pr_number lookup (live row only).
SELECT id, tenant_id, project_id, pr_number, branch, commit_sha, status,
       url, build_id, release_id, repo_full_name, installation_id,
       expires_at, cell_id, created_at, updated_at, deleted_at
FROM preview_environments
WHERE tenant_id = $1 AND project_id = $2 AND pr_number = $3
  AND deleted_at IS NULL;
