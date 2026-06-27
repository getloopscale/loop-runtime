-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopZero Authors

-- M2: retire one-subscription-per-tenant; enforce one-subscription-per-Account.
-- account_id is fully backfilled by 000041, so UNIQUE(account_id) holds.
-- tenant_id stays NOT NULL (the originating tenant) but is no longer unique.
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_tenant_id_key;
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_account_id_key UNIQUE (account_id);
