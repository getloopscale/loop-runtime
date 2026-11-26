-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000025_memory_webhooks
--
-- Plan 3 of dashboard-memory-section-roadmap.md
-- (see .claude/knowledge-base/plans/2026-06-06-memory-operations-plan.md)
--
-- Customer-facing memory webhook registrations. Distinct from the existing
-- `webhook_events` table (Stripe webhook ingestion — see 000001_initial_schema).
-- Webhook DELIVERY is NOT implemented at v0.1 — the dashboard discloses this
-- via a persistent "Demo only" Alert per Plan 3 ADR D2 (EC-14 absorption).
--
-- ADRs honored:
--   D2 (Plan 3) — registration is shipped, delivery is honestly demo-only
--   D5 (Plan 0) — tenant scoping via (subject, tenant_id) discriminator
--   D7 (Plan 0) — soft-delete via revoked_at; hard DELETE forbidden
--
-- Forward-only per CLAUDE.md convention.
-- ============================================================================

CREATE TABLE IF NOT EXISTS memory_webhooks (
    id          TEXT PRIMARY KEY,                                              -- mwh-{16hex}
    subject     TEXT NOT NULL,                                                  -- auth.GetUser.Subject (Ory subject)
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    url         TEXT NOT NULL CHECK (url ~* '^https?://'),                      -- defense in depth at DB layer
    label       TEXT NOT NULL CHECK (length(label) BETWEEN 1 AND 64),
    events      JSONB NOT NULL,                                                 -- e.g. ["memory.remembered","memory.forgotten"]
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at  TIMESTAMPTZ,                                                    -- soft-delete (D7); NULL = active
    CHECK (jsonb_typeof(events) = 'array')
);

-- Partial index covers the list-handler hot path (filtered by tenant + subject,
-- excluding revoked). Mirrors the idx_loop_runtime_api_keys_tenant shape from P0.
CREATE INDEX IF NOT EXISTS idx_memory_webhooks_owner
    ON memory_webhooks (tenant_id, subject)
    WHERE revoked_at IS NULL;
