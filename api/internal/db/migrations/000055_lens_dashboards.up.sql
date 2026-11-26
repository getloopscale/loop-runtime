-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000055_lens_dashboards
--
-- loopruntime-lens M66 — per-workspace custom dashboards for the /v1/dashboard/lens BFF.
-- A dashboard is an ordered list of widgets (KPI tile / trend chart / top-N table)
-- each bound to an existing M51/M60 observability series; the whole board is stored
-- as ONE JSONB `config` blob (KISS — the handler validates the widget palette + caps
-- at the write boundary; opik's single-config-blob precedent). Consumed/queried by the
-- loopruntime-cloud LensDashboardsHandler (raw database/sql) — loopruntime-cloud does not run
-- migrations, the engine owns schema.
--
-- Tenancy DIVERGES from 000053_lens_saved_views: a custom dashboard is a SHARED team
-- artifact (every member of the workspace sees/edits it — the peer-standard model:
-- opik/langfuse scope dashboards to the workspace/project, not the user), so it is
-- scoped by `tenant_id` ALONE. `created_by` records the authoring subject for audit
-- (never a scope filter). A cross-tenant read/write collapses to 404 (existence-leak
-- prevention) in the handler, never 403.
--
-- Forward-only per CLAUDE.md convention.
-- ============================================================================

CREATE TABLE IF NOT EXISTS lens_dashboards (
    id          TEXT PRIMARY KEY,                                       -- ldb-{16hex}
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE, -- workspace scope
    created_by  TEXT NOT NULL,                                          -- auth.GetUser.Subject (audit only)
    name        TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
    config      JSONB NOT NULL,                                         -- ordered widget list (validated at the boundary)
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Backs the per-workspace list scan (WHERE tenant_id = $1 ORDER BY created_at DESC).
CREATE INDEX IF NOT EXISTS idx_lens_dashboards_tenant
    ON lens_dashboards (tenant_id);
