-- Returns the primary key to `(key)` — global, as it was.
--
-- A real rollback, not a no-op: nothing depends on the new key, and leaving half a
-- decision behind is worse than returning the state the up migration found.
--
-- CAUTION when running: if two tenants have already written the SAME key under the
-- widened key, this `ADD CONSTRAINT` fails — and failing is the correct behaviour.
-- Narrowing a unique key can collide; the error names exactly the rows the old
-- version could not represent.
BEGIN;

ALTER TABLE idempotency_keys
  DROP CONSTRAINT IF EXISTS idempotency_keys_pkey;

ALTER TABLE idempotency_keys
  ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (key);

COMMIT;
