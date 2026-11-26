-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000036_loop_runtime_api_keys_account_id
--
-- M0: add NULLABLE account_id to loop_runtime_api_keys (additive). The key carries its
-- Account; ResolveAPIKey reads it (NULL-tolerant for legacy keys). Nullable
-- now; NOT NULL deferred to M3.
-- ============================================================================

ALTER TABLE loop_runtime_api_keys
    ADD COLUMN IF NOT EXISTS account_id TEXT REFERENCES accounts(id);

CREATE INDEX IF NOT EXISTS idx_loop_runtime_api_keys_account_id ON loop_runtime_api_keys(account_id);
