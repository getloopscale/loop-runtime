-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000040_subscriptions_account_id
--
-- M2 of the account-level-tenancy roadmap (billing per-Account).
-- Adds NULLABLE account_id to subscriptions (additive, backward-compatible).
-- Nullable until 000041 backfills; NOT NULL in 000043. The account becomes the
-- billing boundary (one subscription per account); the tenant stays the data
-- isolation boundary, unchanged.
-- ============================================================================

ALTER TABLE subscriptions
    ADD COLUMN IF NOT EXISTS account_id TEXT REFERENCES accounts(id);

CREATE INDEX IF NOT EXISTS idx_subscriptions_account_id ON subscriptions(account_id);
