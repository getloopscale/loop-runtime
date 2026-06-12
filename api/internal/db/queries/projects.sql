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

-- name: ListProjectsWithSummaryByTenant :many
-- Returns all projects for a tenant with per-project summary fields:
--   - environment_count: total env_targets rows (all kinds) scoped to
--     this (tenant_id, project.name) tuple
--   - last_deploy_status_raw: status string of the most recent build
--     (ordered by created_at DESC, id DESC tie-breaker per EC-8) OR
--     empty string when the project has zero builds
--   - last_deploy_at: timestamp of the most recent build (NULL when no
--     builds exist) — T5.3 LIVE-bug fix 2026-06-08; dashboard list page
--     was rendering "—" for every project's Last deploy column because
--     T2.1 originally dropped the legacy `last_deploy` field
--   - apps_count: jsonb_array_length of the most recent build's apps_spec
--     (0 when no builds OR empty apps_spec) — T5.3 LIVE-bug fix; was
--     hardcoded to 0 on the list because legacy `apps[]` was dropped
--   - latest_framework_raw: first app's `type` field from the latest build's
--     apps_spec (empty string fallback) — closest semantic to the legacy
--     `framework` indicator. loopzero.yaml apps schema has `type` (server/
--     static/worker/etc.), not `framework`; the dashboard renders as a
--     Badge so the type string is the canonical "what is this app" label
-- Per plan 2026-06-08-dashboard-screens-review-fixes T2.1 + ADR D2 +
-- EC-1/EC-8 absorptions, extended 2026-06-08 for T5.3 sponsor walkthrough
-- LIVE-bug. Caller wraps last_deploy_status_raw through
-- pkg/enforcement.MapBuildStatusToProjectStatus to derive the 5-value
-- dashboard ProjectStatus.
-- Join keys match the "project_id stores slug, not UUID" convention
-- (CLAUDE.md "Common Mistakes → Project name vs project ID"): both
-- builds.project_id and environment_targets.project_id are the project
-- name slug, joined against projects.name. Only projects.id is UUID.
SELECT
  p.id,
  p.name,
  p.tenant_id,
  p.created_by,
  p.created_at,
  COALESCE(env_count.cnt, 0)::bigint AS environment_count,
  COALESCE(latest_build.status, '')::text AS last_deploy_status_raw,
  latest_build.created_at AS last_deploy_at,
  COALESCE(jsonb_array_length(latest_build.apps_spec), 0)::bigint AS apps_count,
  COALESCE(latest_build.apps_spec->0->>'type', '')::text AS latest_framework_raw
FROM projects p
LEFT JOIN LATERAL (
  SELECT COUNT(*) AS cnt
  FROM environment_targets et
  WHERE et.tenant_id = p.tenant_id AND et.project_id = p.name
) env_count ON true
LEFT JOIN LATERAL (
  SELECT b.status, b.created_at, b.apps_spec
  FROM builds b
  WHERE b.tenant_id = p.tenant_id AND b.project_id = p.name
  ORDER BY b.created_at DESC, b.id DESC
  LIMIT 1
) latest_build ON true
WHERE p.tenant_id = $1
ORDER BY p.name;

-- name: DeleteProjectByID :execrows
-- Tenant-scoped delete. Wrapper maps 0 rows-affected → "project not
-- found" error (legacy contract).
DELETE FROM projects
WHERE tenant_id = $1 AND id = $2;
