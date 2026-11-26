-- A named, auditable role for the control plane — replacing an implicit owner bypass.
--
-- loopruntime-cloud shares this database with the engine ("The cloud service shares the
-- database with the engine API. It does NOT run migrations", cmd/main.go:359) and
-- connects as `loopruntime` — the OWNER. That predates migration 000062 and survives it,
-- because 000062 gave the owner a maintenance policy so migrations keep working.
--
-- The result is that the control plane reads every tenant, and does so through a
-- privilege that is invisible: nothing in pg_policy says "the control plane may
-- cross tenants", it simply happens because of who it connects as.
--
-- WHAT THIS CHANGES, AND WHAT IT DOES NOT
--
-- It changes WHO and HOW VISIBLY, not HOW MUCH. `loopruntime_cloud` gets the same
-- cross-tenant reach the owner already had — the control plane genuinely needs
-- it: listing an account's tenants, aggregating billing, evaluating alerts
-- across workspaces. What improves is that the reach is now
--
--   * NAMED — it appears in pg_policy, so an auditor can see it and enumerate it;
--   * NARROWER — the role holds no DDL, so a compromised control plane cannot
--     alter a policy, drop FORCE, or create a new bypassing role, all of which
--     the owner can do today;
--   * REVOCABLE — dropping one policy removes the reach for one table, without
--     touching schema ownership.
--
-- It does NOT make the control plane's per-tenant routes isolated. Those still
-- rest on the `WHERE tenant_id = $n` predicate they already carry, which is the
-- weaker guarantee migration 000062 exists to replace. Closing that requires
-- binding a request scope in the Go layer, the way the TypeScript products do,
-- and is deliberately NOT attempted here — a half-bound scope would fail closed
-- on the cross-tenant routes and read as an outage.
--
-- So: this is a real reduction in blast radius and a real gain in auditability.
-- It is not tenant isolation for loopruntime-cloud, and calling it that would misreport
-- the posture.
--
-- OPERATIONAL PRE-CONDITION: after applying this, point the control plane's
-- DATABASE_URL at `loopruntime_cloud`. Until that happens it still runs as the owner
-- and none of the above applies — db.AssertControlPlaneRole refuses to boot on
-- the owner so the gap cannot pass unnoticed.

BEGIN;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loopruntime_cloud') THEN
        BEGIN
            CREATE ROLE loopruntime_cloud WITH LOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
        EXCEPTION WHEN insufficient_privilege THEN
            RAISE NOTICE 'could not create loopruntime_cloud — runner lacks CREATEROLE (expected in local dev)';
        END;
    END IF;
END $$;

DO $$
DECLARE
    t             text;
    tenant_tables text[] := ARRAY[
        'app_scaling_config', 'audit_log', 'builds', 'cost_counters',
        'custom_domains', 'deploy_events', 'environment_targets',
        'github_installations', 'idempotency_keys', 'lens_alert_rules',
        'lens_saved_views', 'memory_exports', 'memory_webhooks',
        'metering_cursor', 'preview_environments', 'preview_slot',
        'project_repositories', 'projects', 'release_app_attestations',
        'releases', 'secrets', 'subscriptions', 'tenant_members',
        'tenant_tombstones', 'tenants_pending_deletion', 'loopruntime_api_keys',
        'usage_records', 'workspace_webhook_deliveries', 'workspace_webhooks',
        'tenants'
    ];
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loopruntime_cloud') THEN
        RAISE NOTICE 'loopruntime_cloud absent — skipping its policies';
        RETURN;
    END IF;

    -- current_database(), not a literal: the database is named `loopruntime` in
    -- production and something else in every CI job and audit replay. A
    -- hardcoded name makes the migration fail with `database "loopruntime" does not
    -- exist` exactly where the isolation is being measured.
    EXECUTE format('GRANT CONNECT ON DATABASE %I TO loopruntime_cloud', current_database());
    EXECUTE 'GRANT USAGE ON SCHEMA public TO loopruntime_cloud';
    EXECUTE 'GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO loopruntime_cloud';
    EXECUTE 'GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO loopruntime_cloud';
    EXECUTE 'ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO loopruntime_cloud';

    FOREACH t IN ARRAY tenant_tables LOOP
        IF NOT EXISTS (
            SELECT 1 FROM information_schema.tables
            WHERE table_schema = 'public' AND table_name = t
        ) THEN
            CONTINUE;
        END IF;

        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', t || '_cloud_cross_tenant', t);
        EXECUTE format($f$
            CREATE POLICY %I ON public.%I
              TO loopruntime_cloud
              USING (true) WITH CHECK (true)
        $f$, t || '_cloud_cross_tenant', t);
    END LOOP;
END $$;

COMMIT;
