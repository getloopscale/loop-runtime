-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000037_backfill_accounts
--
-- M0: create exactly ONE default Account per existing tenant owner, so that
-- 000038/000039 can link pre-existing tenants and keys. Idempotent via
-- ON CONFLICT (owner_id) DO NOTHING (the UNIQUE(owner_id) from 000034).
-- gen_random_bytes requires pgcrypto.
-- ============================================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;

INSERT INTO accounts (id, display_name, plan, owner_id)
SELECT
    'acct-' || encode(gen_random_bytes(8), 'hex'),
    COALESCE(NULLIF(u.display_name, ''), u.email, u.id),
    'starter',
    u.id
FROM (SELECT DISTINCT owner_id FROM tenants) d
JOIN users u ON u.id = d.owner_id
ON CONFLICT (owner_id) DO NOTHING;
