-- queries/preview_slot.sql — sqlc query definitions for the preview_slot
-- table.
--
-- 2026-06-07 single-slot-per-project model (migration 000028):
--   PK = (tenant_id, project_id) — Netlify-real semantics.
--   Each project of a tenant has its OWN slot. Substitution (D3) only
--   replaces the slot of the SAME project. PR in project B no longer
--   silently overwrites PR in project A.
--
-- Multi-tenant: every query carries `tenant_id` predicate (preview_slot
-- is in TenantOwnedTables). RLS policy enforced at row level.

-- name: GetPreviewSlot :one
-- Returns the preview slot for a specific (tenant, project) tuple.
-- NoRows when the project has never had a preview — wrapper returns
-- (nil, nil) per legacy contract.
SELECT tenant_id, project_id, active_pr_number, active_branch,
       active_release_id, url, updated_at
FROM preview_slot WHERE tenant_id = $1 AND project_id = $2;

-- name: ListPreviewSlotsByTenant :many
-- Returns all preview slots for a tenant, one row per project that has
-- ever had a preview (active_pr_number may be NULL for closed slots).
-- Used by the dashboard /environments grid to render N preview rows
-- — one per project under the same tenant.
SELECT tenant_id, project_id, active_pr_number, active_branch,
       active_release_id, url, updated_at
FROM preview_slot WHERE tenant_id = $1
ORDER BY project_id;

-- name: UpsertPreviewSlot :exec
-- D3 substitute behavior PER PROJECT: insert or update atomically.
-- ON CONFLICT (tenant_id, project_id) DO UPDATE replaces the previous
-- active PR of the SAME project. PR in another project NEVER
-- touches the slot of this project.
INSERT INTO preview_slot (
  tenant_id, project_id, active_pr_number, active_branch,
  active_release_id, url
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (tenant_id, project_id)
DO UPDATE SET
  active_pr_number  = EXCLUDED.active_pr_number,
  active_branch     = EXCLUDED.active_branch,
  active_release_id = EXCLUDED.active_release_id,
  url               = EXCLUDED.url,
  updated_at        = NOW();

-- name: ClearPreviewSlot :exec
-- D6 cleanup chain: called when the active PR of a project closes.
-- NULLs the active_* fields for the (tenant, project) row.
-- The slot row remains (for audit) but all active_* are NULL —
-- dashboard renders "No active branch" for that project.
UPDATE preview_slot
SET active_pr_number  = NULL,
    active_branch     = NULL,
    active_release_id = NULL,
    url               = NULL,
    updated_at        = NOW()
WHERE tenant_id = $1 AND project_id = $2;

-- name: EnsurePreviewSlotEmpty :exec
-- Idempotent variant of ClearPreviewSlot — inserts an empty slot if
-- none exists for the (tenant, project), else clears active_* fields.
-- Used by project provisioning to guarantee every project has a
-- preview slot row (D1 consequence: preview category is always present).
INSERT INTO preview_slot (tenant_id, project_id)
VALUES ($1, $2)
ON CONFLICT (tenant_id, project_id)
DO UPDATE SET
  active_pr_number  = NULL,
  active_branch     = NULL,
  active_release_id = NULL,
  url               = NULL,
  updated_at        = NOW();
