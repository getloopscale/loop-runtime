-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- M2: every existing subscription inherits its tenant's Account (1 tenant : 1
-- account today, so backfill is collision-free).
UPDATE subscriptions s
SET account_id = t.account_id
FROM tenants t
WHERE t.id = s.tenant_id
  AND s.account_id IS NULL;
