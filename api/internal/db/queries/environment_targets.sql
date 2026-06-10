-- queries/environment_targets.sql — sqlc query definitions for the
-- environment_targets table (the operational pointer that tracks
-- desired/active release per (tenant, project, environment) tuple).
--
-- T2.1 (Batch A #6) of plan docs/plans/2026-05-29-sqlc-migration-plan.md
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
-- legacy contract.
SELECT tenant_id, project_id, environment, desired_release_id,
       active_release_id, updated_by, correlation_id, updated_at
FROM environment_targets
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3;

-- name: ListEnvironmentTargetsByProject :many
-- Tenant + project scoped listing. Empty result → empty slice (not
-- nil) per legacy contract.
SELECT tenant_id, project_id, environment, desired_release_id,
       active_release_id, updated_by, correlation_id, updated_at
FROM environment_targets
WHERE tenant_id = $1 AND project_id = $2;

-- name: DeleteEnvironmentTarget :exec
-- Tenant-scoped delete. Idempotent: missing target → no-op (legacy
-- contract).
DELETE FROM environment_targets
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3;
