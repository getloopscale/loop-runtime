-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

DROP INDEX IF EXISTS idx_subscriptions_account_id;
ALTER TABLE subscriptions DROP COLUMN IF EXISTS account_id;
