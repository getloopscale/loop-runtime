-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000050_workspace_webhooks
--
-- J-16 Settings — customer-facing workspace/deploy webhook registrations.
-- Mirrors the approved 000025_memory_webhooks schema for a new resource: these
-- webhooks fire on DEPLOY events (deploy.succeeded / deploy.failed / …) rather
-- than memory events. Consumed by the loop-console WorkspaceWebhooksHandler.
--
-- Distinct from `webhook_events` (Stripe ingestion, 000001) and `memory_webhooks`
-- (memory events, 000025). Webhook DELIVERY is NOT implemented in this v0.1 — the
-- dashboard discloses this via a persistent "Delivery coming soon" Alert, exactly
-- as memory_webhooks shipped (ADR D2 precedent). The delivery worker + deploy-event
-- trigger is a separate T2-gated slice.
--
-- ADRs honored (mirrored from memory_webhooks):
--   D2 — registration shipped, delivery honestly deferred
--   D5 — tenant scoping via (subject, tenant_id) discriminator
--   D7 — soft-delete via revoked_at; hard DELETE forbidden
--
-- Forward-only per CLAUDE.md convention.
-- ============================================================================

CREATE TABLE IF NOT EXISTS workspace_webhooks (
    id          TEXT PRIMARY KEY,                                              -- wwh-{16hex}
    subject     TEXT NOT NULL,                                                  -- auth.GetUser.Subject (Ory subject)
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    url         TEXT NOT NULL CHECK (url ~* '^https?://'),                      -- defense in depth at DB layer
    label       TEXT NOT NULL CHECK (length(label) BETWEEN 1 AND 64),
    events      JSONB NOT NULL,                                                 -- e.g. ["deploy.succeeded","deploy.failed"]
    secret      TEXT NOT NULL,                                                  -- whsec_<...>; per-webhook HMAC-SHA256 signing secret (shown once)
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at  TIMESTAMPTZ,                                                    -- soft-delete (D7); NULL = active
    CHECK (jsonb_typeof(events) = 'array')
);

-- Partial index covers the list-handler hot path (filtered by tenant + subject,
-- excluding revoked). Mirrors idx_memory_webhooks_owner.
CREATE INDEX IF NOT EXISTS idx_workspace_webhooks_owner
    ON workspace_webhooks (tenant_id, subject)
    WHERE revoked_at IS NULL;
