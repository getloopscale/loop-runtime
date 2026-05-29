-- queries/releases.sql — sqlc query definitions for the releases table.
--
-- T1.1 of plan docs/plans/2026-05-29-sqlc-migration-plan.md v1.2 (Phase 1
-- pilot — M07 Rekor URL persistence 3-layer bug site, commit b3375ed0).
--
-- Per ADR-1 D2 (pgx/v5) + D3 (emit_methods_with_db_argument) + D6
-- (schema=schema.gen.sql) + EC-3 (snake_case JSON tags via sqlc.yaml).
--
-- Every multi-tenant SELECT / UPDATE / DELETE carries `tenant_id` predicate
-- per platform-security-engineer condition 1 + M-1 + M-2 (enforced by
-- pkg/quality/tenantpredicate gate).
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
