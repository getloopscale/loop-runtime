-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopZero Authors

-- ============================================================================
-- 000027_memory_webhooks_add_secret
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
-- ============================================================================

ALTER TABLE memory_webhooks ADD COLUMN secret TEXT;

COMMENT ON COLUMN memory_webhooks.secret IS
    'HMAC-SHA256 signing secret. Format: whsec_<43 chars base64url> = 48 chars total. '
    'Persisted raw — the dispatcher signs at delivery time. '
    'NULL means a pre-delivery-plan webhook (Plan 3 demo-only era): the dispatcher '
    'logs a warning and omits the X-LoopZero-Signature header. Customer must revoke + '
    'recreate to obtain a signed webhook.';
