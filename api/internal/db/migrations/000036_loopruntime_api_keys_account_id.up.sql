-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000036_loopruntime_api_keys_account_id
--
-- M0: add NULLABLE account_id to loopruntime_api_keys (additive). The key carries its
-- Account; ResolveAPIKey reads it (NULL-tolerant for legacy keys). Nullable
-- now; NOT NULL deferred to M3.
-- ============================================================================

ALTER TABLE loopruntime_api_keys
    ADD COLUMN IF NOT EXISTS account_id TEXT REFERENCES accounts(id);

CREATE INDEX IF NOT EXISTS idx_loopruntime_api_keys_account_id ON loopruntime_api_keys(account_id);
