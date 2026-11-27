-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000026_memory_exports
--
-- Plan 3 of dashboard-memory-section-roadmap.md
-- (see .claude/knowledge-base/plans/2026-06-06-memory-operations-plan.md)
--
-- Async export jobs — the customer requests a JSON dump of their memories; a
-- background worker writes the file; the customer downloads.
--
-- ADRs honored:
--   D1 (Plan 3) — id is server-generated UUID format (`export-{36-char UUID}`);
--                 client `filename` body is IGNORED; defense at DB+handler layers
--   D5 (Plan 0) — tenant scoping via (subject, tenant_id) discriminator
--
-- Forward-only per CLAUDE.md convention.
-- ============================================================================

CREATE TABLE IF NOT EXISTS memory_exports (
    id            TEXT PRIMARY KEY,                                            -- export-{uuid v4}
    subject       TEXT NOT NULL,
    tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    display_name  TEXT,                                                         -- optional customer label (NOT the filename — D1)
    status        TEXT NOT NULL CHECK (status IN ('pending','running','succeeded','failed')),
    file_path     TEXT,                                                         -- worker-written on succeeded
    error_message TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at  TIMESTAMPTZ
);

-- Listing hot path: WHERE tenant_id + subject ORDER BY created_at DESC.
CREATE INDEX IF NOT EXISTS idx_memory_exports_owner_active
    ON memory_exports (tenant_id, subject, created_at DESC);
