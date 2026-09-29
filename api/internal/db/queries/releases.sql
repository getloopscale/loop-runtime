-- queries/releases.sql — sqlc query definitions for the releases table.
--
-- T1.1 of plan 2026-05-29-sqlc-migration-plan v1.2 (Phase 1
-- pilot — M07 Rekor URL persistence 3-layer bug site, commit b3375ed0).
--
-- Per ADR-1 D2 (pgx/v5) + D3 (emit_methods_with_db_argument) + D6
-- (schema=schema.gen.sql) + EC-3 (snake_case JSON tags via sqlc.yaml).
--
-- Every multi-tenant SELECT / UPDATE / DELETE carries `tenant_id` predicate
-- per platform-security-engineer condition 1 + M-1 + M-2 (enforced by
-- cmd/loopzero-ops/internal/quality/tenantpredicate gate).
--
-- Initial query set (5/13 total — remaining 8 queries added incrementally as
-- callers migrate per T1.1 sub-step):
--   - InsertRelease         (Create + RETURNING)
--   - GetReleaseByID        (cross-tenant; used by reaper/internal)
--   - GetReleaseByIDForTenant (tenant-isolated)
--   - UpdateReleaseStatus   (state machine integration; wrapper layer
--                            calls releaseMachine.SafeTransition before
--                            invoking this UPDATE per D4)
--   - GetActiveByEnv        (canonical per-env active release query)
--
-- The M07 bug-class column `image_attestation_rekor_url` is included in
-- every column list — sqlc enforces compile-time consistency.

-- name: InsertRelease :one
INSERT INTO releases (
  id,
  build_id,
  tenant_id,
  project_id,
  environment,
  status,
  correlation_id,
  created_by,
  created_at,
  git_commit,
  git_mirror_status,
  apps,
  artifacts,
  side_effects,
  image_digest,
  image_signature,
  image_signed_at,
  image_attestation_rekor_url
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
)
RETURNING *;

-- tenantpredicate:exempt
-- name: GetReleaseByID :one
-- Cross-tenant lookup; used by background reapers + internal admin paths.
-- Multi-tenant isolation guaranteed by callers (release IDs are non-guessable).
SELECT * FROM releases WHERE id = $1;

-- name: GetReleaseByIDForTenant :one
-- Tenant-isolated lookup; the canonical "user requests release by ID" path.
SELECT * FROM releases
WHERE id = $1 AND tenant_id = $2;

-- name: UpdateReleaseStatus :one
-- Updates status + optional context (correlation_id propagation).
-- The wrapper layer MUST call releaseMachine.SafeTransition BEFORE invoking
-- this UPDATE per D4 (state-machine invariant preserved at store layer).
UPDATE releases
SET status = $1
WHERE id = $2 AND tenant_id = $3
RETURNING *;

-- name: GetActiveByEnv :one
-- Returns the currently-ACTIVE release for a (tenant, project, environment)
-- tuple. There is at most one ACTIVE release per env at any time (invariant
-- enforced by the state machine + the WHERE clause below).
SELECT * FROM releases
WHERE tenant_id = $1
  AND project_id = $2
  AND environment = $3
  AND status = 'ACTIVE'
ORDER BY created_at DESC
LIMIT 1;

-- name: GetPreviousRelease :one
-- Returns the most-recent SUPERSEDED-or-ROLLED-BACK release for a (tenant,
-- project, env) tuple — the canonical "what should we rollback to?" query.
SELECT * FROM releases
WHERE tenant_id = $1
  AND project_id = $2
  AND environment = $3
  AND status IN ('SUPERSEDED', 'ROLLED_BACK')
ORDER BY created_at DESC
LIMIT 1;

-- tenantpredicate:exempt
-- name: GetLatestByBuildID :one
-- Returns the most-recent release for a given build_id. Used by the release
-- inspector (T4.4 of plan 2026-05-28-meet-001) + reconcile loop. Cross-tenant
-- because build IDs are globally unique and the caller validates tenant
-- ownership before invoking.
SELECT * FROM releases
WHERE build_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: ListReleasesByEnv :many
-- Paginated list of releases for a (tenant, project, env) tuple. Includes
-- ARCHIVED unless caller filters. Limit is enforced at the caller.
SELECT * FROM releases
WHERE tenant_id = $1
  AND project_id = $2
  AND environment = $3
ORDER BY created_at DESC
LIMIT $4;

-- name: SetReleaseGitCommit :one
-- Records the git commit SHA on a release after the gitops push completes.
-- Idempotent: re-applying with the same value is a no-op.
UPDATE releases
SET git_commit = $1
WHERE id = $2 AND tenant_id = $3
RETURNING *;

-- name: AppendReleaseSideEffect :one
-- Merges a JSON object into the release's side_effects column using the
-- jsonb `||` concat operator. Repeated keys overwrite; others preserve.
-- Used by the reconcile reaper to capture transition reasons (T4.1 of plan
-- 2026-04-platform-fix-all-issues).
UPDATE releases
SET side_effects = COALESCE(side_effects, '{}'::jsonb) || $1::jsonb
WHERE id = $2 AND tenant_id = $3
RETURNING *;

-- name: ListReleaseStatusesByEnv :many
-- The distinct statuses present for a (tenant, project, env) tuple. The
-- project-delete cascade reads them so the release state machine, not SQL,
-- decides which of them move to CANCELLED.
SELECT DISTINCT status FROM releases
WHERE tenant_id = $1
  AND project_id = $2
  AND environment = $3;

-- name: CancelReleasesByEnv :many
-- Cancels the releases of a (tenant, project, env) tuple whose status is in
-- from_statuses. The caller computes from_statuses from the release state
-- machine (ProjectDeleted edges), so the transition table never leaks here.
UPDATE releases
SET status = 'CANCELLED'
WHERE tenant_id = @tenant_id
  AND project_id = @project_id
  AND environment = @environment
  AND status = ANY(@from_statuses::text[])
RETURNING id;

-- tenantpredicate:exempt
-- name: FindPendingReleasesByCell :many
-- Cell-scoped reaper query: returns PENDING releases in a cell, or in every cell
-- when the cell is empty (how the API runs its loops, #447). The cell_id
-- filter provides the multi-cell isolation (callers are reapers; they intentionally
-- scan across tenants within a cell). Roadmap 03 multi-cell.
SELECT * FROM releases
WHERE (sqlc.arg(cell_id)::text = '' OR cell_id = sqlc.arg(cell_id))
  AND status = 'PENDING'
ORDER BY created_at ASC
LIMIT 100;

-- tenantpredicate:exempt
-- name: FindActiveReleasesByCell :many
-- Cell-scoped convergence-loop health query. DISTINCT ON returns the most-
-- recent ACTIVE release per (tenant, project, env). MEET-2026-03-31-023 T1-4.
SELECT DISTINCT ON (tenant_id, project_id, environment)
       *
FROM releases
WHERE (sqlc.arg(cell_id)::text = '' OR cell_id = sqlc.arg(cell_id))
  AND status = 'ACTIVE'
ORDER BY tenant_id, project_id, environment, created_at DESC
LIMIT 100;

-- name: FindPendingReleasesByTenantEnv :many
-- Callback-driven activation path (CODE-SCAN-001 fix). Tenant-scoped and
-- per-env, more efficient than the cell-scoped FindPendingReleases when a
-- specific build's callback fires.
SELECT * FROM releases
WHERE tenant_id = $1
  AND environment = $2
  AND status = 'PENDING'
ORDER BY created_at ASC
LIMIT 100;

-- name: ListLatestActiveReleasePerEnv :many
-- The most recent ACTIVE release per environment across all of a tenant's
-- projects: the dashboard's per-environment last-deploy KPI.
SELECT DISTINCT ON (environment) environment, git_commit, created_at
FROM releases
WHERE tenant_id = $1 AND status = 'ACTIVE'
ORDER BY environment, created_at DESC;
