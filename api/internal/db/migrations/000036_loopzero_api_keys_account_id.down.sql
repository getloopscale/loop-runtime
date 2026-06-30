-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopZero Authors

DROP INDEX IF EXISTS idx_loopzero_api_keys_account_id;
ALTER TABLE loopzero_api_keys DROP COLUMN IF EXISTS account_id;
