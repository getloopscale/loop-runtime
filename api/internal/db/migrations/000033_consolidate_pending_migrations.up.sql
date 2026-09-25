-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopZero Authors

-- ============================================================================
-- 000033_consolidate_pending_migrations
--
-- CANONICAL CONSOLIDATION 2026-06-15 (dev-public Cleanroom blocker fix):
--
-- Prior state had THREE migrations duplicated across 2 different version
-- numbers each (identical content, leftover from incomplete merge
-- conflict renumbers between branches). golang-migrate rejected the
-- directory with `duplicate migration file` errors.
--
-- Per sponsor request 2026-06-15 (translated):
--   "Fix the migrations (you may consolidate them into a single one if you want)"
--
-- This single migration replaces the following 6 duplicate copies that
-- are deleted in the same commit:
--   - 000006_add_stripe_subscription_item_id (dup of 000022)
--   - 000022_add_stripe_subscription_item_id (dup of 000006)
--   - 000027_memory_webhooks_add_secret (dup of 000029)
--   - 000029_memory_webhooks_add_secret (dup of 000027)
--   - 000028_memory_webhook_deliveries (dup of 000030)
--   - 000030_memory_webhook_deliveries (dup of 000028)
--
-- Each of the 3 unique migrations below is idempotent via IF NOT
-- EXISTS / forward-only ADD COLUMN — so this consolidation applies
-- cleanly to BOTH fresh databases (where the original copies never
-- ran) AND databases that already applied one of the dup copies via
-- a prior partial migration run.
--
-- All 3 follow the CLAUDE.md forward-only convention: no DROP, only
-- ADD COLUMN IF NOT EXISTS / CREATE TABLE IF NOT EXISTS / CREATE
-- INDEX IF NOT EXISTS.
--
-- Forward-only per CLAUDE.md convention.
-- ============================================================================

-- ----------------------------------------------------------------------------
-- Part 1 of 3: add_stripe_subscription_item_id
--
-- Add the Stripe subscription ITEM id (si_xxx) to subscriptions.
--
-- Metered usage is reported to Stripe at
-- /v1/subscription_items/{id}/usage_records, which requires a
-- subscription ITEM id (si_xxx), NOT a subscription id (sub_xxx).
-- The metering reporter previously sent stripe_subscription_id, so
-- every usage report was rejected and metered revenue was silently
-- lost (find_data_001).
--
-- This column is populated from the customer.subscription.updated
-- webhook (items[0].id). It is nullable: existing subscriptions
-- backfill on their next subscription.updated event, and the reporter
-- skips (with a WARN) any row whose item id is still null rather than
-- reporting against the wrong identifier.
--
-- Forward-only, additive, idempotent. No DROP.
-- ----------------------------------------------------------------------------

ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS stripe_subscription_item_id TEXT;

-- ----------------------------------------------------------------------------
-- Part 2 of 3: memory_webhooks_add_secret
--
-- Memory Webhook Delivery plan, T5.2
-- (see .claude/knowledge-base/plans/2026-06-06-memory-webhook-delivery-plan.md)
--
-- Adds the per-webhook HMAC-SHA256 signing secret (ADR D1).
--
-- ADR D1 consequence: nullable. Pre-this-plan webhooks (created during
-- Plan 3 when delivery was demo-only) keep NULL; the dispatcher logs a
-- warning and skips signing for those rows. Customers must revoke +
-- recreate to obtain a signed webhook. Future migration can NOT-NULL
-- once telemetry shows all pre-plan webhooks are gone.
--
-- Forward-only per CLAUDE.md convention.
-- ----------------------------------------------------------------------------

ALTER TABLE memory_webhooks ADD COLUMN IF NOT EXISTS secret TEXT;

COMMENT ON COLUMN memory_webhooks.secret IS
    'HMAC-SHA256 signing secret. Format: whsec_<43 chars base64url> = 48 chars total. '
    'Persisted raw — the dispatcher signs at delivery time. '
    'NULL means a pre-delivery-plan webhook (Plan 3 demo-only era): the dispatcher '
    'logs a warning and omits the X-LoopZero-Signature header. Customer must revoke + '
    'recreate to obtain a signed webhook.';

-- ----------------------------------------------------------------------------
-- Part 3 of 3: memory_webhook_deliveries
--
-- Memory Webhook Delivery plan, T5.3
-- (see .claude/knowledge-base/plans/2026-06-06-memory-webhook-delivery-plan.md)
--
-- Per-delivery audit row. One row per logical delivery (NOT one row
-- per HTTP attempt — attempt_count counter tracks retries within a
-- single row).
--
-- ADRs locked:
--   D2 — status state machine: pending → delivering → delivered |
--        failed → dead_letter (after 9 attempts per exp backoff
--        schedule)
--   D3 — separate deliveries table (NOT counter on memory_webhooks);
--        rowwise per-delivery audit; FK CASCADE on memory_webhooks so
--        revoke cleans up
--   D4 — dispatcher hot path = `WHERE next_attempt_at <= NOW() AND
--        status IN ('pending','failed')` — partial index optimizes
--
-- Forward-only per CLAUDE.md convention.
-- ----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS memory_webhook_deliveries (
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
CREATE INDEX IF NOT EXISTS idx_memory_webhook_deliveries_due
    ON memory_webhook_deliveries (next_attempt_at)
    WHERE status IN ('pending', 'failed');

-- Dashboard history view: list last 10 deliveries per webhook.
-- DESC ordering means the hot end of the BTree is the recent end.
CREATE INDEX IF NOT EXISTS idx_memory_webhook_deliveries_webhook
    ON memory_webhook_deliveries (webhook_id, created_at DESC);
