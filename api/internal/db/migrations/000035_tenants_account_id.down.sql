-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

DROP INDEX IF EXISTS idx_tenants_account_id;
ALTER TABLE tenants DROP COLUMN IF EXISTS account_id;
