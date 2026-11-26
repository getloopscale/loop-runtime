-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000052_workspace_webhook_deliveries
--
-- J-16 Settings — workspace/deploy webhook DELIVERY queue (slice 3, consumer).
--
-- loopruntime-cloud drains the deploy_events outbox (migration 000051), matches each
-- event to the subscribed workspace_webhooks (000050), and inserts one row here
-- per (webhook, event). The delivery worker (loopruntime-cloud, mirror of
-- memory_webhook_worker) claims due rows, POSTs to the customer URL through the
-- SSRF-guarded client (safehttp, loopruntime-cloud#149), and records the outcome.
--
-- Engine-owned DDL (all migrations live in the engine, shared DB) but WRITTEN
-- and READ exclusively by loopruntime-cloud — the engine never references this table
-- (decoupling invariant, MEET-2026-07-03-005).
--
-- Mirrors memory_webhook_deliveries (000033) and ADDS, per the slice-3
-- distributed-systems review:
--   * transition_id — the dedup key copied from deploy_events (one value per
--     ENTRY into ACTIVE). UNIQUE(webhook_id, transition_id) makes the cloud
--     fan-out idempotent: re-draining the same event never double-enqueues.
--   * tenant_id — enables per-tenant fairness in the claim (no global-order
--     starvation) and per-tenant delivery quotas.
--   * claimed_at / claimed_by — a LEASE. The worker sets them on claim; a
--     visibility-timeout reaper resets rows stuck in 'delivering' past the
--     lease (worker crash / shutdown mid-dispatch), closing the permanent
--     'delivering'-orphan class the memory worker has.
--
-- NO FK on transition_id → deploy_events: outbox rows are reaped after drain,
-- but their deliveries must outlive them (retry/dead-letter history).
--
-- Forward-only per CLAUDE.md convention.
-- ============================================================================

CREATE TABLE IF NOT EXISTS workspace_webhook_deliveries (
    id                   TEXT PRIMARY KEY,                                            -- wwd-{16hex}
    webhook_id           TEXT NOT NULL REFERENCES workspace_webhooks(id) ON DELETE CASCADE,
    transition_id        TEXT NOT NULL,                                               -- from deploy_events; fan-out dedup key
    tenant_id            TEXT NOT NULL,                                               -- per-tenant fairness + quotas
    event_type           TEXT NOT NULL,                                               -- deploy.succeeded | deploy.failed
    payload              JSONB NOT NULL,                                              -- customer-safe body POSTed to the consumer
    status               TEXT NOT NULL CHECK (status IN ('pending','delivering','delivered','failed','dead_letter')),
    attempt_count        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at      TIMESTAMPTZ,                                                 -- NULL = not scheduled (terminal state)
    claimed_at           TIMESTAMPTZ,                                                 -- lease: set on claim, cleared/reaped on timeout
    claimed_by           TEXT,                                                        -- worker replica id holding the lease
    last_response_status INTEGER,
    last_response_body   TEXT,                                                        -- truncated to 4 KiB at the worker
    last_error_message   TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at         TIMESTAMPTZ,                                                 -- populated on delivered | dead_letter
    UNIQUE (webhook_id, transition_id)                                               -- idempotent fan-out: one delivery per (webhook, event)
);

-- Dispatcher hot path: due rows (pending/failed), oldest first.
CREATE INDEX IF NOT EXISTS idx_workspace_webhook_deliveries_due
    ON workspace_webhook_deliveries (next_attempt_at)
    WHERE status IN ('pending', 'failed');

-- Reaper hot path: rows stuck in 'delivering' past the lease timeout.
CREATE INDEX IF NOT EXISTS idx_workspace_webhook_deliveries_lease
    ON workspace_webhook_deliveries (claimed_at)
    WHERE status = 'delivering';

-- Dashboard history view: last N deliveries per webhook, recent first.
CREATE INDEX IF NOT EXISTS idx_workspace_webhook_deliveries_webhook
    ON workspace_webhook_deliveries (webhook_id, created_at DESC);
