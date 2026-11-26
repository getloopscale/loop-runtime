-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000039_link_keys_account
--
-- M0: link each pre-existing API key to its tenant's Account (a key inherits
-- the Account of its tenant). Only touches rows still NULL.
-- ============================================================================

UPDATE loopruntime_api_keys k
SET account_id = t.account_id
FROM tenants t
WHERE t.id = k.tenant_id
  AND k.account_id IS NULL;
