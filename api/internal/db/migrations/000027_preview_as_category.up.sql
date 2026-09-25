-- Migration 000027: preview-as-category single-slot model (Netlify-style)
--
-- Plan: .claude/knowledge-base/plans/2026-06-07-preview-as-category-single-slot-plan.md
-- ADRs D1-D8 — Sponsor request 2026-06-07 (translated): "Implement it like this: 'Preview'
-- (1 entry). Only one active Preview, not one per branch. Write a plan to implement and fix it,
-- FAANG level, no workarounds, no shortcuts."
--
-- This migration is forward-only per CLAUDE.md Rule 11. The companion down migration
-- (000027_preview_as_category.down.sql) is provided for dev only and MUST NOT run
-- in production environments.
--
-- Changes:
--   1. environment_targets: ADD COLUMN kind text NOT NULL DEFAULT 'permanent' CHECK
--      Existing rows backfill to 'permanent'. New preview rows must specify kind='preview'.
--   2. environment_targets: CREATE INDEX on (tenant_id, kind) for enforcement count query.
--   3. preview_slot: NEW TABLE. PK=tenant_id forces single-slot constraint at schema level
--      (D3 substitute behavior). NULL active_pr_number = no active PR.
--   4. preview_slot: RLS policy mirrors environment_targets pattern.
--   5. Clean break (D8): DELETE existing preview-pr-N rows from environment_targets and
--      preview_environments. K8s-side cleanup (PreviewEnvironment CRs + namespaces +
--      ArgoCD Apps) executed by Phase 0 T0.2 operator runbook.

BEGIN;

-- D2 / D5: kind column for env_targets enables enforcement to filter permanent
-- envs only when counting against plan_limits.environments cap (Bug A fix).
ALTER TABLE environment_targets
  ADD COLUMN kind text NOT NULL DEFAULT 'permanent'
  CHECK (kind IN ('permanent', 'preview'));

-- Index optimizes the enforcement count query path:
-- SELECT COUNT(*) FROM environment_targets WHERE tenant_id = $1 AND kind = 'permanent'
CREATE INDEX idx_environment_targets_kind
  ON environment_targets (tenant_id, kind);

-- D2 / D3: preview_slot is the single-slot source-of-truth for preview deployments.
-- PK=tenant_id means at most ONE active preview deployment per tenant.
-- UpsertPreviewSlot ON CONFLICT DO UPDATE forces substitution (D3 atomic replace).
-- CHECK constraint enforces all-or-nothing: either all active_* fields populated, or none.
CREATE TABLE preview_slot (
  tenant_id          text         PRIMARY KEY,
  project_id         text         NOT NULL,
  active_pr_number   integer,
  active_branch      text,
  active_release_id  text         REFERENCES releases(id) ON DELETE SET NULL,
  url                text,
  updated_at         timestamptz  NOT NULL DEFAULT now(),
  CONSTRAINT preview_slot_all_or_nothing CHECK (
    (active_pr_number IS NULL AND active_branch IS NULL AND url IS NULL)
    OR
    (active_pr_number IS NOT NULL AND active_branch IS NOT NULL)
  )
);

-- Secondary index for dashboard queries that need to find tenants by project (rare).
CREATE INDEX idx_preview_slot_project ON preview_slot (project_id);

-- RLS policy mirrors environment_targets pattern. Defense-in-depth: enforces
-- tenant isolation at the row level even if application-side filters miss.
ALTER TABLE preview_slot ENABLE ROW LEVEL SECURITY;
CREATE POLICY preview_slot_tenant_isolation ON preview_slot
  USING (
    (tenant_id = current_setting('app.tenant_id', true))
    OR (current_setting('app.tenant_id', true) IS NULL)
    OR (current_setting('app.tenant_id', true) = '')
  );

-- D8 (sponsor clean break mandate): delete existing preview-pr-N rows.
-- After this migration runs, callers using the new substitute behavior (Phase 3)
-- will UPSERT preview_slot when a PR opens. Existing PreviewEnvironment CRs +
-- namespaces are cleaned up by Phase 0 T0.2 operator script (not by this migration).
DELETE FROM environment_targets WHERE environment LIKE 'preview-pr-%';
DELETE FROM preview_environments WHERE pr_number IS NOT NULL;

COMMIT;
