-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopZero Authors

-- ============================================================================
-- 000036_loopzero_api_keys_account_id
--
-- M0: add NULLABLE account_id to loopzero_api_keys (additive). The key carries its
-- Account; ResolveAPIKey reads it (NULL-tolerant for legacy keys). Nullable
-- now; NOT NULL deferred to M3.
-- ============================================================================

ALTER TABLE loopzero_api_keys
    ADD COLUMN IF NOT EXISTS account_id TEXT REFERENCES accounts(id);

CREATE INDEX IF NOT EXISTS idx_loopzero_api_keys_account_id ON loopzero_api_keys(account_id);
