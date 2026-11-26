-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000051_deploy_events
--
-- J-16 Settings — workspace/deploy webhook DELIVERY (slice 3), foundation.
--
-- Transactional-outbox table (Option C, ratified MEET-2026-07-03-005). The
-- ENGINE writes exactly one row here, INSIDE the release-activation /
-- build-failed transaction, when a deploy reaches a terminal state
-- (deploy.succeeded / deploy.failed). Writing on the same tx as the state
-- change makes event capture atomic with the deploy — no lost or duplicate
-- events on crash.
--
-- loopruntime-cloud (NOT the engine) drains this table and fans each event out to the
-- matching `workspace_webhooks` (migration 000050) as delivery rows. Per the
-- decoupling invariant, the ENGINE never references `workspace_webhooks`; it
-- only emits a generic domain event here. The engine writes; loopruntime-cloud reads
-- and marks `drained_at`; a reaper deletes drained rows.
--
-- `transition_id` is the dedup key for the cloud fan-out — one value per ENTRY
-- into ACTIVE, NOT `release_id`: the Release machine re-enters ACTIVE after a
-- rollback-recovery, and each such entry is a legitimately distinct
-- deploy.succeeded event. UNIQUE(transition_id) guards against a double INSERT
-- for the same transition (the activation kernel's idempotency guard already
-- prevents re-activating the same PENDING→ACTIVE, so this is defense in depth).
--
-- payload carries ONLY customer-safe fields (deploy_id, project, status,
-- timestamp, event) — no internal terms (Rule 3), enforced at the emit site.
--
-- This migration is the schema foundation ONLY. The kernel INSERT (engine emit)
-- and the loopruntime-cloud drain + delivery worker are separate slices gated on
-- MEET-2026-07-03-005's remaining conditions (SSRF guard already shipped via
-- loopruntime-cloud#149, Security/SRE guardian sign-off).
--
-- Forward-only per CLAUDE.md convention.
-- ============================================================================

CREATE TABLE IF NOT EXISTS deploy_events (
    id             BIGSERIAL PRIMARY KEY,
    transition_id  TEXT NOT NULL UNIQUE,                                        -- per-entry-into-ACTIVE; cloud fan-out dedup key
    event_type     TEXT NOT NULL CHECK (event_type IN ('deploy.succeeded', 'deploy.failed')),
    tenant_id      TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id     TEXT NOT NULL,
    deploy_id      TEXT NOT NULL,                                               -- release_id (succeeded) or build_id (failed)
    correlation_id TEXT,                                                        -- forwarded engine → delivery (Correlation-ID invariant)
    payload        JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),     -- customer-safe fields only (Rule 3)
    occurred_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    drained_at     TIMESTAMPTZ                                                  -- loopruntime-cloud sets after fan-out; reaper deletes drained rows
);

-- Cloud drain hot path: undrained events, oldest first.
CREATE INDEX IF NOT EXISTS idx_deploy_events_undrained
    ON deploy_events (id)
    WHERE drained_at IS NULL;
