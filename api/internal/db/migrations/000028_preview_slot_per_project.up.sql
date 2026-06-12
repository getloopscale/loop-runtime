-- Migration 000028: preview_slot PK from per-tenant to per-project
--
-- Rationale (sponsor mandate 2026-06-07): the original 000027 used
-- PK=(tenant_id) which caused PR in project B to silently overwrite
-- the slot of project A for the same tenant. Netlify-real semantics
-- are "1 preview per SITE (project)" — N projects in a tenant get
-- N independent slots. Vercel-style multi-URL stays rejected (K8s
-- cost not justified).
--
-- This migration:
--   1. Drops the per-tenant PK on preview_slot
--   2. Adds a per-project PK (tenant_id, project_id)
--
-- Data preservation: existing rows are unique on (tenant_id) so they
-- are also unique on (tenant_id, project_id) — zero conflicts.

BEGIN;

-- Drop the legacy PK constraint.
ALTER TABLE preview_slot DROP CONSTRAINT preview_slot_pkey;

-- Add the new per-project PK.
ALTER TABLE preview_slot ADD PRIMARY KEY (tenant_id, project_id);

COMMIT;
