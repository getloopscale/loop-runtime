-- queries/projects.sql — sqlc query definitions for the projects
-- table.
--
-- T2.1 (Batch A #4) of plan docs/plans/2026-05-29-sqlc-migration-plan.md
-- v1.3. Multi-tenant — every query carries `tenant_id` predicate
-- (projects is in TenantOwnedTables). ProjectStore is consumed by 5
-- prod call sites via struct-literal `&db.PostgresProjectStore{DB:
-- dbAdapter}` — preserved in T2.1 #4.
--
-- Schema: projects table has UNIQUE constraint on (tenant_id, name)
-- — re-create with same name surfaces as 23505 unique_violation
-- which the wrapper maps to domain.ErrProjectExists per legacy
-- contract.

-- name: InsertProject :exec
-- Caller supplies id (generated via GenerateProjectID() helper).
-- 23505 on (tenant_id, name) UNIQUE → domain.ErrProjectExists in
-- wrapper.
INSERT INTO projects (id, tenant_id, name, created_by)
VALUES ($1, $2, $3, $4);

-- name: EnsureProject :exec
-- Idempotent upsert used by the deploy flow to guarantee the project
-- is registered in the projects table. ON CONFLICT (tenant_id, name)
-- DO NOTHING preserves the original row when a redeploy fires after
-- the project was deleted via `loopzero project delete` and then
-- recreated.
INSERT INTO projects (id, tenant_id, name, created_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (tenant_id, name) DO NOTHING;

-- name: ListProjectsByTenant :many
-- Returns all projects for a tenant, ordered by name (consistent
-- output for `loopzero project list`).
SELECT id, name, tenant_id, created_by, created_at
FROM projects
WHERE tenant_id = $1
ORDER BY name;

-- name: GetProjectByName :one
-- Tenant-scoped lookup. NoRows → wrapper returns (nil, nil) per
-- legacy contract — caller decides whether absence is an error.
SELECT id, name, tenant_id, created_by, created_at
FROM projects
WHERE tenant_id = $1 AND name = $2;

-- name: DeleteProjectByID :execrows
-- Tenant-scoped delete. Wrapper maps 0 rows-affected → "project not
-- found" error (legacy contract).
DELETE FROM projects
WHERE tenant_id = $1 AND id = $2;
