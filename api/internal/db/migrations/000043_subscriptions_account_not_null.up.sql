-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- M2 Phase-2 enforce: account_id NOT NULL after the 000041 backfill. Pre-flight
-- guard fails loudly if any NULL remains (never silently flip).
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM subscriptions WHERE account_id IS NULL) THEN
    RAISE EXCEPTION 'subscriptions.account_id has NULLs — run 000041 backfill first';
  END IF;
END $$;
ALTER TABLE subscriptions ALTER COLUMN account_id SET NOT NULL;
