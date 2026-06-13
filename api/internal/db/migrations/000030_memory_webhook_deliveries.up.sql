-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopZero Authors

-- ============================================================================
-- 000028_memory_webhook_deliveries
--
-- Memory Webhook Delivery plan, T5.3
-- (see .claude/knowledge-base/plans/2026-06-06-memory-webhook-delivery-plan.md)
--
-- Per-delivery audit row. One row per logical delivery (NOT one row per
-- HTTP attempt — attempt_count counter tracks retries within a single row).
--
-- ADRs locked:
--   D2 — status state machine: pending → delivering → delivered | failed
--        → dead_letter (after 9 attempts per exp backoff schedule)
--   D3 — separate deliveries table (NOT counter on memory_webhooks); rowwise
--        per-delivery audit; FK CASCADE on memory_webhooks so revoke cleans up
--   D4 — dispatcher hot path = `WHERE next_attempt_at <= NOW() AND status IN
--        ('pending','failed')` — partial index optimizes
--
-- Forward-only per CLAUDE.md convention.
-- ============================================================================

CREATE TABLE memory_webhook_deliveries (
    id                     TEXT PRIMARY KEY,                                          -- mwd-{16hex}
    webhook_id             TEXT NOT NULL REFERENCES memory_webhooks(id) ON DELETE CASCADE,
    event_type             TEXT NOT NULL,                                              -- e.g. memory.forgotten
    payload                JSONB NOT NULL,                                             -- the JSON body POSTed to the consumer
    status                 TEXT NOT NULL CHECK (status IN ('pending','delivering','delivered','failed','dead_letter')),
    attempt_count          INTEGER NOT NULL DEFAULT 0,
    next_attempt_at        TIMESTAMPTZ,                                                -- NULL = not scheduled (terminal state)
    last_response_status   INTEGER,                                                    -- HTTP status code of last attempt
    last_response_body     TEXT,                                                       -- truncated to 4 KiB at handler level
    last_error_message     TEXT,                                                       -- transport error message
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at           TIMESTAMPTZ                                                 -- populated on terminal transition (delivered or dead_letter)
);

-- Dispatcher hot path: poll for due rows every 5 seconds.
-- Partial index avoids scanning terminal-state rows (delivered + dead_letter).
CREATE INDEX idx_memory_webhook_deliveries_due
    ON memory_webhook_deliveries (next_attempt_at)
    WHERE status IN ('pending', 'failed');

-- Dashboard history view: list last 10 deliveries per webhook.
-- DESC ordering means the hot end of the BTree is the recent end.
CREATE INDEX idx_memory_webhook_deliveries_webhook
    ON memory_webhook_deliveries (webhook_id, created_at DESC);
