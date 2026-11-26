-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- M2: retire one-subscription-per-tenant; enforce one-subscription-per-Account.
-- account_id is fully backfilled by 000041, so UNIQUE(account_id) holds.
-- tenant_id stays NOT NULL (the originating tenant) but is no longer unique.

-- Pre-flight guard: fail loudly if any Account already owns >1 subscription.
-- Without it, ADD CONSTRAINT below would fail with an opaque
-- "could not create unique index" — this names the offending account_id instead.
-- (NULLs are not caught here on purpose: Postgres allows multiple NULLs under a
-- UNIQUE constraint, and 000043's NOT NULL guard is the gate for those.)
DO $$
DECLARE
  dup TEXT;
BEGIN
  SELECT account_id INTO dup
  FROM subscriptions
  WHERE account_id IS NOT NULL
  GROUP BY account_id
  HAVING count(*) > 1
  LIMIT 1;
  IF dup IS NOT NULL THEN
    RAISE EXCEPTION 'subscriptions has >1 row for account_id=% — resolve the duplicate before enforcing UNIQUE(account_id)', dup;
  END IF;
END $$;

ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_tenant_id_key;
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_account_id_key UNIQUE (account_id);
