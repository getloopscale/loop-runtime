-- Reverting this migration RESTORES a standing cross-tenant bypass for the role
-- that runs it. Read the up migration before doing so.
--
-- This is deliberately a no-op rather than a re-creation of the policies.
--
-- A faithful `down` would run `CREATE POLICY <t>_owner_maintenance ... TO CURRENT_USER
-- USING (true)` again, and that is precisely the privilege 000064 exists to remove:
-- unnamed, permanent, applying to every row of every tenant, and bound to whichever
-- role happened to run the migration. Shipping that as an automated rollback would
-- make one `migrate down` enough to silently reopen it.
--
-- Nothing depends on the policies being back. The isolation policy from 000062 has no
-- `TO` clause, so it is `TO PUBLIC` and already covers the owner: set `app.tenant_id`
-- and the owner reads and writes normally. If an operator genuinely needs the old
-- behaviour during an incident, the honest path is an explicit, logged, time-boxed
-- statement typed by a human who knows what it grants — not a rollback step that
-- restores it as a side effect of moving the schema version back.

SELECT 1;
