-- The idempotency key was unique GLOBALLY, and the one who picks it is the client.
--
-- `idempotency_keys_pkey` covered `(key)` and nothing else. The value comes from the
-- request's `Idempotency-Key` header — arbitrary text the client makes up. Two
-- consequences, and the second is the one that matters:
--
--   1. Two clients that pick the same string compete for the same primary key.
--      This is not a remote hypothesis: `deploy-1`, today's date, the branch name. The
--      second one gets a uniqueness violation AFTER the work has already run —
--      the record is written after the response —, so the side effect
--      happened and the request ends in an error.
--   2. The namespace can be occupied on purpose. A client can write keys
--      until it covers what another one would probably use, and deny it service
--      without touching anything of its own.
--
-- What did NOT happen: responses leaking across tenants. The read
-- (`GetIdempotencyRecord`) always matched `key = $1 AND tenant_id = $2`, so
-- nobody ever received the response recorded by someone else. The defect is one of
-- availability and coupling, not confidentiality — and saying which of the two it
-- is matters more than the scare.
--
-- Uniqueness was always per tenant; the column simply was not in the key.
-- Widening it never creates a collision: every row unique under `(key)` stays
-- unique under `(tenant_id, key)`.
--
-- Forward-only. No FK points at this table (verified in pg_constraint),
-- so swapping the primary key has no cascading effect.
--
-- @see api/internal/db/generated/idempotency.sql.go — the queries, already scoped
-- @see api/internal/db/migrations/000062_rls_fail_closed.up.sql — the table's policy

BEGIN;

ALTER TABLE idempotency_keys
  DROP CONSTRAINT IF EXISTS idempotency_keys_pkey;

ALTER TABLE idempotency_keys
  ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (tenant_id, key);

COMMIT;
