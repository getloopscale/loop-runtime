-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000038_link_tenants_account
--
-- M0: link each pre-existing tenant to its owner's backfilled Account.
-- Only touches rows still NULL (idempotent; new tenants already carry it).
-- ============================================================================

UPDATE tenants t
SET account_id = a.id
FROM accounts a
WHERE a.owner_id = t.owner_id
  AND t.account_id IS NULL;
