-- Down 000028 — DEV ONLY (forward-only per CLAUDE.md Rule 11).
--
-- WARNING: dropping the (tenant_id, project_id) PK and re-adding
-- (tenant_id) only works when at most ONE row per tenant exists.
-- This is the case immediately after a fresh up→down cycle but NOT
-- after any preview activity. Run only in dev cleanroom cycles.

BEGIN;

ALTER TABLE preview_slot DROP CONSTRAINT preview_slot_pkey;
ALTER TABLE preview_slot ADD PRIMARY KEY (tenant_id);

COMMIT;
