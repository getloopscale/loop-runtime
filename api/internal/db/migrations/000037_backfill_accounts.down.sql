-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopZero Authors

-- Reverses the backfill: remove accounts whose owner owns a tenant (i.e. the
-- rows this migration created). App-created accounts for owners without a
-- tenant are not touched. In a full M0 rollback, 000034.down drops the table
-- afterwards anyway.
DELETE FROM accounts a
WHERE EXISTS (SELECT 1 FROM tenants t WHERE t.owner_id = a.owner_id);
