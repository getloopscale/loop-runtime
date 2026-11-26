-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

DROP INDEX IF EXISTS idx_loop_runtime_api_keys_account_id;
ALTER TABLE loop_runtime_api_keys DROP COLUMN IF EXISTS account_id;
