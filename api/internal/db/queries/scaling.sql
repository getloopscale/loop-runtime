-- queries/scaling.sql — sqlc query definitions for the
-- app_scaling_config table (per-app HPA / scale-to-zero
-- configuration).
--
-- T2.1 (Batch A #7) of plan 2026-05-29-sqlc-migration-plan
-- v1.3. Multi-tenant: every query carries `tenant_id` predicate
-- (app_scaling_config is in TenantOwnedTables). Plan T2.1 of
-- pre-launch-gaps-plan.md replaced the stub-202 path; the wrapper
-- here preserves the idempotent hash-compare short-circuit + UPSERT.

-- name: GetScalingConfigByApp :one
-- Tenant-scoped lookup. NoRows → wrapper maps to
-- ErrScalingConfigNotFound (legacy sentinel).
SELECT id, tenant_id, project_id, environment, app_name,
       min_replicas, max_replicas, target_cpu_pct, scale_to_zero,
       updated_at, updated_by, config_hash, target_memory_pct
FROM app_scaling_config
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3 AND app_name = $4;

-- name: UpsertScalingConfig :one
-- ON CONFLICT (tenant_id, project_id, environment, app_name) DO
-- UPDATE refreshes all body fields + updated_at = NOW() +
-- updated_by + config_hash. Returns the (id, updated_at) tuple so
-- the wrapper can update the incoming struct in place.
INSERT INTO app_scaling_config (
  tenant_id, project_id, environment, app_name,
  min_replicas, max_replicas, target_cpu_pct, scale_to_zero,
  updated_by, config_hash, target_memory_pct
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
ON CONFLICT (tenant_id, project_id, environment, app_name)
DO UPDATE SET
  min_replicas = EXCLUDED.min_replicas,
  max_replicas = EXCLUDED.max_replicas,
  target_cpu_pct = EXCLUDED.target_cpu_pct,
  target_memory_pct = EXCLUDED.target_memory_pct,
  scale_to_zero = EXCLUDED.scale_to_zero,
  updated_at = NOW(),
  updated_by = EXCLUDED.updated_by,
  config_hash = EXCLUDED.config_hash
RETURNING id, updated_at;

-- name: ListScalingConfigsByEnv :many
-- Tenant-scoped listing for (tenant, project, environment) tuple,
-- ordered by app_name for consistent output.
SELECT id, tenant_id, project_id, environment, app_name,
       min_replicas, max_replicas, target_cpu_pct, scale_to_zero,
       updated_at, updated_by, config_hash, target_memory_pct
FROM app_scaling_config
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
ORDER BY app_name;

-- name: DeleteScalingConfig :exec
-- Tenant-scoped single-row delete. Idempotent (silent on missing).
DELETE FROM app_scaling_config
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3 AND app_name = $4;

-- name: DeleteScalingConfigsByProject :exec
-- Cascade-delete for project deletion: removes EVERY scaling row
-- tied to (tenant, project) regardless of environment / app.
-- Prevents a future project of the same name from inheriting stale
-- replicas / HPA state (which would surface as
-- REPLICAS_EXCEED_PLAN on a clean redeploy). Idempotent.
DELETE FROM app_scaling_config
WHERE tenant_id = $1 AND project_id = $2;

-- name: CountScalingConfigsByTenant :one
-- Total rows for plan-cap enforcement at the API layer (free-tier
-- limit).
SELECT count(*) FROM app_scaling_config
WHERE tenant_id = $1;
