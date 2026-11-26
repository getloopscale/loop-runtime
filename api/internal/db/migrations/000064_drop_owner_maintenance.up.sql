-- Removes the owner's standing cross-tenant bypass introduced by migration 000062.
--
-- WHAT 000062 DID, AND WHY IT WAS WRONG
--
-- It created `<table>_owner_maintenance` on all 30 tenant-scoped tables plus
-- `tenants`, each `TO CURRENT_USER USING (true) WITH CHECK (true)`. The stated
-- reasoning was that FORCE ROW LEVEL SECURITY against a table owner is not a real
-- boundary, since the owner holds DDL and could lift FORCE anyway.
--
-- That conflates two very different things. Lifting FORCE is a deliberate DDL
-- statement somebody has to write and a reviewer can see in a diff. A standing
-- `USING (true)` is a bypass on every query, silently, forever. The first leaves a
-- trace; the second is the absence of a boundary dressed as a policy.
--
-- IT WAS ALSO UNNECESSARY, which is the part that settles it. The isolation policy
-- from 000062 is created with NO `TO` clause, which makes it `TO PUBLIC`: it already
-- applies to every role, the owner included. Under FORCE the owner is simply treated
-- like any other role — it must set `app.tenant_id`, and if it does, everything
-- works. `tenantScopedPool` sets it on every connection it hands out, and migrations
-- and credential resolution deliberately use the raw pool because they legitimately
-- have no tenant.
--
-- So the maintenance policy added a second, permanent, unnamed way across every
-- tenant to solve a problem the first policy did not have.
--
-- A THIRD DEFECT, worth recording because it is easy to miss: `TO CURRENT_USER`
-- resolves at CREATE POLICY time to the literal role name that ran the migration.
-- The policies were therefore bound to whoever migrated, and could differ between
-- environments — redundant AND non-deterministic.
--
-- WHAT THIS DOES NOT BREAK. Measured before writing this migration:
--   * zero migrations in this repository contain UPDATE/DELETE, so no schema change
--     depends on the bypass;
--   * exactly ONE Go path does DML outside a request — `audit_log_pruner.go` — and
--     it runs inside loopruntime-api, which `db.AssertRequestPoolIsolated` forces to connect
--     as a non-owner. It never held this bypass, so removing it changes nothing for
--     the pruner. (That pruner deleting zero rows under RLS is a real defect, tracked
--     separately; it is not caused by, and not fixed by, this migration.)
--
-- The named cross-tenant policies stay: `*_worker_cross_tenant` (000062) and
-- `*_cloud_cross_tenant` (000063). Both name their role in `TO`, both appear in
-- pg_policy under that name, and both are revocable one table at a time. Cross-tenant
-- reach is allowed here — being unnamed is what is not.
--
-- The list is derived from the catalogue rather than hand-written: a table added to
-- 000062's array later would otherwise keep its bypass silently.

BEGIN;

DO $$
DECLARE
    r record;
    dropped int := 0;
BEGIN
    FOR r IN
        SELECT tablename, policyname
          FROM pg_policies
         WHERE schemaname = 'public'
           AND policyname LIKE '%\_owner\_maintenance'
    LOOP
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', r.policyname, r.tablename);
        dropped := dropped + 1;
    END LOOP;

    RAISE NOTICE 'dropped % owner-maintenance policies', dropped;
END $$;

COMMIT;
