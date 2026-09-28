-- queries/environment_targets.sql — sqlc query definitions for the
-- environment_targets table (the operational pointer that tracks
-- desired/active release per (tenant, project, environment) tuple).
--
-- T2.1 (Batch A #6) of plan 2026-05-29-sqlc-migration-plan
-- v1.3. Multi-tenant: every query carries `tenant_id` predicate
-- (environment_targets is in TenantOwnedTables).
--
-- Per spec §3 (Source of Truth) + §9 (table). All mutating queries
-- run inside the env lock transaction (Rule 6); sqlctxprop gate
-- validates tx propagation.

-- name: UpsertEnvironmentTargetDesired :exec
-- ON CONFLICT (tenant_id, project_id, environment) DO UPDATE refreshes
-- desired_release_id + updated_by + correlation_id + updated_at. First
-- call inserts; subsequent calls update. The same desired_release_id /
-- updated_by / correlation_id values are passed in both VALUES and the
-- DO UPDATE clause (legacy duplicated $4-6 / $7-9 form preserved).
INSERT INTO environment_targets (
  tenant_id, project_id, environment, desired_release_id, updated_by, correlation_id
) VALUES (
  $1, $2, $3, $4, $5, $6
)
ON CONFLICT (tenant_id, project_id, environment)
DO UPDATE SET desired_release_id = $4, updated_by = $5, correlation_id = $6, updated_at = NOW();

-- name: UpdateEnvironmentTargetActive :exec
-- Called AFTER ArgoCD confirms sync — never before. Tenant predicate
-- enforced.
UPDATE environment_targets
SET active_release_id = $1, updated_at = NOW()
WHERE tenant_id = $2 AND project_id = $3 AND environment = $4;

-- name: GetEnvironmentTarget :one
-- Tenant-scoped lookup. NoRows → wrapper returns (nil, nil) per
-- legacy contract. SELECT * to keep Row type aliased to
-- generated.EnvironmentTarget (sqlc inlines when select-list ==
-- table). Includes `kind` for D5 enforcement + D7 API merge.
SELECT tenant_id, project_id, environment, desired_release_id,
       active_release_id, updated_by, correlation_id, updated_at, kind,
       last_replicas_ready_value, last_replicas_ready_at
FROM environment_targets
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3;

-- name: ListEnvironmentTargetsByProject :many
-- Tenant + project scoped listing. Empty result → empty slice (not
-- nil) per legacy contract. SELECT includes `kind` (see
-- GetEnvironmentTarget rationale).
SELECT tenant_id, project_id, environment, desired_release_id,
       active_release_id, updated_by, correlation_id, updated_at, kind,
       last_replicas_ready_value, last_replicas_ready_at
FROM environment_targets
WHERE tenant_id = $1 AND project_id = $2;

-- name: DeleteEnvironmentTarget :exec
-- Tenant-scoped delete. Idempotent: missing target → no-op (legacy
-- contract).
DELETE FROM environment_targets
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3;

-- name: UpsertPreviewEnvironmentTarget :exec
-- D2 (plan 2026-06-07-preview-as-category): explicit kind='preview' insert.
-- Used by PreviewService when the preview build commits a release. The
-- `kind` column is immutable post-create (D2 consequence) — repeat inserts
-- for the same (tenant, project, environment) update release tracking but
-- keep kind='preview'. Distinct from UpsertEnvironmentTargetDesired which
-- defaults kind='permanent'.
INSERT INTO environment_targets (
  tenant_id, project_id, environment, kind,
  desired_release_id, updated_by, correlation_id
) VALUES (
  $1, $2, $3, 'preview', $4, $5, $6
)
ON CONFLICT (tenant_id, project_id, environment)
DO UPDATE SET
  desired_release_id = $4,
  updated_by         = $5,
  correlation_id     = $6,
  updated_at         = NOW();

-- name: CountPermanentEnvironments :one
-- Plan enforcement: counts a tenant's permanent (project, environment) tuples,
-- gated against plan_limits.environments. M2 v1.2 interprets that cap per
-- tuple, and the primary key makes each tuple one row, so COUNT(*) is the
-- per-tuple count (DISTINCT environment would collapse web/prod and api/prod
-- into one). Preview environments live in preview_slot and are NOT counted.
SELECT COUNT(*)::int FROM environment_targets
WHERE tenant_id = $1 AND kind = 'permanent';

-- name: ListPermanentEnvironments :many
-- D7 helper (Bug B fix): dashboard /environments grid lists DISTINCT
-- permanent envs for a tenant. Preview slot is fetched via GetPreviewSlot
-- and merged on the API side into the {permanent[], preview{}} response.
SELECT DISTINCT environment FROM environment_targets
WHERE tenant_id = $1 AND kind = 'permanent'
ORDER BY environment;

-- name: DeleteEnvironmentTargetsByKind :exec
-- D6 cleanup chain: used by PreviewService.DeletePreviewByPR to remove
-- preview rows on PR close. Idempotent (DELETE matches 0 or 1 row).
DELETE FROM environment_targets
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3 AND kind = $4;

-- name: RecordEnvironmentTargetReplicasReady :execrows
-- Data-plane readiness snapshot written by the api's readiness poller
-- (environments.ReadinessPoller) from Prometheus. Touches ONLY the two
-- observation columns the dashboard badge reads, so it never contends with
-- the release pointers the env lock guards. Zero rows affected means the
-- (tenant, project, environment) has no target row — the caller reports it.
-- Cross-tenant by design: runs on the background (loopzero_worker) pool.
UPDATE environment_targets
SET last_replicas_ready_value = $1, last_replicas_ready_at = $2
WHERE tenant_id = $3 AND project_id = $4 AND environment = $5;
