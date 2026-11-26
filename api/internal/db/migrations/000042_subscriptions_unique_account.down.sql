-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_account_id_key;
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_tenant_id_key UNIQUE (tenant_id);
