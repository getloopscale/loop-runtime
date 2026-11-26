-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Unlink keys from accounts (re-NULL). Pairs with 000039 up.
UPDATE loop_runtime_api_keys SET account_id = NULL WHERE account_id IS NOT NULL;
