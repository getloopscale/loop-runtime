-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000053_lens_saved_views
--
-- loopruntime-lens M52 — per-tenant saved trace-list filter combinations for the
-- /v1/dashboard/lens BFF. A saved view stores the nuqs-serialized filter query
-- string verbatim; "apply" pushes it back onto the URL (the dashboard's nuqs
-- parsers hydrate it). Consumed/queried by the loopruntime-cloud LensSavedViewsHandler
-- (raw database/sql) — loopruntime-cloud does not run migrations, the engine owns schema.
--
-- Mirrors 000050_workspace_webhooks tenancy: a (subject, tenant_id) discriminator
-- scopes every row to the authenticated Principal (personal per-user views;
-- sharing is via the URL itself, so no cross-user table sharing is needed).
--
-- Distinct from workspace_webhooks (a saved view has no secret/delivery, so — unlike
-- webhooks' soft-delete audit trail — a hard DELETE is correct: a saved view is a
-- disposable user preference, KISS).
--
-- Forward-only per CLAUDE.md convention.
-- ============================================================================

CREATE TABLE IF NOT EXISTS lens_saved_views (
    id            TEXT PRIMARY KEY,                                       -- lsv-{16hex}
    subject       TEXT NOT NULL,                                          -- auth.GetUser.Subject (Ory subject)
    tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name          TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
    query_string  TEXT NOT NULL CHECK (length(query_string) BETWEEN 1 AND 4096),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Backs the per-Principal list + delete scans (WHERE subject = $ AND tenant_id = $).
CREATE INDEX IF NOT EXISTS idx_lens_saved_views_owner
    ON lens_saved_views (tenant_id, subject);
