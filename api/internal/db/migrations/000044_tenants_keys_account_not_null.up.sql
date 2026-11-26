-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- M3 Phase-2 enforce: account_id NOT NULL on tenants + loop_runtime_api_keys after the
-- M0 backfill (000038 links tenants, 000039 links keys). Pre-flight guards fail
-- loudly if any NULL remains (never silently flip — risk-1). One guard per table
-- so the message names the offending table.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM tenants WHERE account_id IS NULL) THEN
    RAISE EXCEPTION 'tenants.account_id has NULLs — run the 000038 backfill before enforcing NOT NULL';
  END IF;
  IF EXISTS (SELECT 1 FROM loop_runtime_api_keys WHERE account_id IS NULL) THEN
    RAISE EXCEPTION 'loop_runtime_api_keys.account_id has NULLs — run the 000039 backfill before enforcing NOT NULL';
  END IF;
END $$;

ALTER TABLE tenants ALTER COLUMN account_id SET NOT NULL;
ALTER TABLE loop_runtime_api_keys ALTER COLUMN account_id SET NOT NULL;
