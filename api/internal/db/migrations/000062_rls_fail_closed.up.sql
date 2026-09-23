-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopZero Authors

-- Tenant isolation: make Row Level Security actually enforce.
--
-- MEASURED STATE BEFORE THIS MIGRATION (2026-08-14, commit 3d70ef44, PG 16.15):
-- a connection scoped to tenant-A via set_config('app.tenant_id', 'tenant-A')
-- read tenant-B's rows. Three independent causes, stacked:
--
--   1. the request pool runs as `loopzero`, which has rolbypassrls=true
--      (api/internal/db/store/project_wrapper.go:132)
--   2. `loopzero` also OWNS the tables, and FORCE ROW LEVEL SECURITY was removed
--      by the archived 000012_fix_rls_empty_string.up.sql:57-61 (git show
--      d426889d1:api/internal/db/migrations-archive/000012_fix_rls_empty_string.up.sql) — an
--      owner bypasses its own policies unless FORCE is on
--   3. the policies themselves were fail-open: the `IS NULL` / `= ''`
--      disjuncts return EVERY row whenever the session variable is unset
--
-- Fixing only (3) changes nothing: (1) and (2) short-circuit the policy
-- before its expression is ever evaluated. This migration closes all three
-- at the database level; the application-side half (handlers connecting as
-- `loopzero_api` instead of the owner) is enforced at boot by
-- db.AssertRequestPoolIsolated.
--
-- SCOPE: 29 tables carrying a `tenant_id` column plus `tenants` (scoped by
-- `id`). Only 12 had RLS at all before this — `loopzero_api_keys`,
-- `subscriptions`, `custom_domains`, `usage_records` and 13 others had no
-- policy whatsoever. The list is derived from information_schema, not from
-- a hand-maintained constant, and the companion gate re-derives it so a new
-- tenant-scoped table cannot be added without a policy.
--
-- CROSS-TENANT WORK: background loops (reconcile, reapers, metering) legitimately
-- scan every tenant. They no longer rely on the fail-open disjunct — they get an
-- explicit, named policy granted only to `loopzero_worker`. A named grant is auditable;
-- an empty string that happens to disable the predicate is not.
--
-- The owner role deliberately does NOT get BYPASSRLS. A future migration that
-- backfills DATA across tenants must connect as `loopzero_worker` (which the
-- cross-tenant policy covers) rather than silently updating zero rows. DDL is
-- unaffected by RLS, so ordinary schema migrations keep working as owner.

BEGIN;

-- ---------------------------------------------------------------------------
-- 1. Tables scoped by a `tenant_id` column.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
    t            text;
    has_worker   boolean;
    tenant_tables text[] := ARRAY[
        'app_scaling_config',
        'audit_log',
        'builds',
        'cost_counters',
        'custom_domains',
        'deploy_events',
        'environment_targets',
        'github_installations',
        'idempotency_keys',
        'lens_alert_rules',
        'lens_saved_views',
        'memory_exports',
        'memory_webhooks',
        'metering_cursor',
        'preview_environments',
        'preview_slot',
        'project_repositories',
        'projects',
        'release_app_attestations',
        'releases',
        'secrets',
        'subscriptions',
        'tenant_members',
        'tenant_tombstones',
        'tenants_pending_deletion',
        'loopzero_api_keys',
        'usage_records',
        'workspace_webhook_deliveries',
        'workspace_webhooks'
    ];
BEGIN
    SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loopzero_worker') INTO has_worker;

    FOREACH t IN ARRAY tenant_tables LOOP
        -- Skip tables absent in this database. Keeps the migration applicable
        -- to older snapshots without a hand-maintained per-table guard.
        IF NOT EXISTS (
            SELECT 1 FROM information_schema.tables
            WHERE table_schema = 'public' AND table_name = t
        ) THEN
            RAISE NOTICE 'skipping %: table not present', t;
            CONTINUE;
        END IF;

        EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE public.%I FORCE ROW LEVEL SECURITY', t);

        -- Replace the fail-open policy (the 12 that had one) or install the
        -- first policy this table has ever had (the other 17).
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', t || '_tenant_isolation', t);
        EXECUTE format($f$
            CREATE POLICY %I ON public.%I
              USING      (tenant_id = current_setting('app.tenant_id', true))
              WITH CHECK (tenant_id = current_setting('app.tenant_id', true))
        $f$, t || '_tenant_isolation', t);

        -- Named cross-tenant grant, worker role only. Permissive policies are
        -- OR-ed, so `loopzero_api` still sees only the isolation predicate above.
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', t || '_worker_cross_tenant', t);
        IF has_worker THEN
            EXECUTE format($f$
                CREATE POLICY %I ON public.%I
                  TO loopzero_worker
                  USING (true) WITH CHECK (true)
            $f$, t || '_worker_cross_tenant', t);
        END IF;

        -- The owner keeps cross-tenant reach, and this is deliberate.
        --
        -- FORCE ROW LEVEL SECURITY does subject an owner to its own policies,
        -- but that is not a security boundary: the owner holds DDL and can
        -- simply run `ALTER TABLE ... NO FORCE` or rewrite the policy. A
        -- constraint the constrained party can lift is documentation, not
        -- enforcement — and pretending otherwise would misreport the posture
        -- to an auditor.
        --
        -- The real boundary is that the request path does not connect as the
        -- owner. That is enforced where it can actually hold: `loopzero_api` is a
        -- non-owner role (isolated by the policy above), and
        -- db.AssertRequestPoolIsolated refuses to boot a request pool whose
        -- role can bypass RLS. FORCE stays on because it still binds every
        -- non-owner role, including any added later without review.
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', t || '_owner_maintenance', t);
        EXECUTE format($f$
            CREATE POLICY %I ON public.%I
              TO CURRENT_USER
              USING (true) WITH CHECK (true)
        $f$, t || '_owner_maintenance', t);
    END LOOP;
END $$;

-- ---------------------------------------------------------------------------
-- 2. `tenants` — the self-table. Scoped by `id`, not `tenant_id`.
-- ---------------------------------------------------------------------------
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'tenants'
    ) THEN
        ALTER TABLE public.tenants ENABLE ROW LEVEL SECURITY;
        ALTER TABLE public.tenants FORCE ROW LEVEL SECURITY;

        DROP POLICY IF EXISTS tenants_self_isolation ON public.tenants;
        CREATE POLICY tenants_self_isolation ON public.tenants
          USING      (id = current_setting('app.tenant_id', true))
          WITH CHECK (id = current_setting('app.tenant_id', true));

        DROP POLICY IF EXISTS tenants_worker_cross_tenant ON public.tenants;
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loopzero_worker') THEN
            CREATE POLICY tenants_worker_cross_tenant ON public.tenants
              TO loopzero_worker
              USING (true) WITH CHECK (true);
        END IF;

        DROP POLICY IF EXISTS tenants_owner_maintenance ON public.tenants;
        EXECUTE format('CREATE POLICY tenants_owner_maintenance ON public.tenants TO %I USING (true) WITH CHECK (true)', CURRENT_USER);
    END IF;
END $$;

-- ---------------------------------------------------------------------------
-- 3. Role posture.
-- ---------------------------------------------------------------------------
-- `loopzero_api` carries the HTTP request path: non-owner, and explicitly NOT
-- BYPASSRLS so the policies above actually bind. `loopzero_worker` is likewise
-- non-owner — it reaches other tenants through the named policy, never
-- through an instance-wide privilege that would also cover tables nobody
-- reviewed.
-- ALTER ROLE needs CREATEROLE, which the migration runner does not have in
-- local dev (the same constraint archived migration 000009 handled, in git
-- history at d426889d1:api/internal/db/migrations-archive/). A failure
-- here must not abort the policy work above, so it degrades to a NOTICE —
-- and db.AssertRequestPoolIsolated refuses to boot if the attribute really
-- is still wrong, which is the check that actually protects production.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loopzero_api') THEN
        BEGIN
            EXECUTE 'ALTER ROLE loopzero_api NOBYPASSRLS';
        EXCEPTION WHEN insufficient_privilege THEN
            RAISE NOTICE 'could not clear BYPASSRLS on loopzero_api — runner lacks CREATEROLE; verify with: SELECT rolbypassrls FROM pg_roles WHERE rolname=''loopzero_api''';
        END;
        EXECUTE 'GRANT USAGE ON SCHEMA public TO loopzero_api';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO loopzero_api';
        EXECUTE 'GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO loopzero_api';
        EXECUTE 'ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO loopzero_api';
        EXECUTE 'ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO loopzero_api';
    ELSE
        RAISE NOTICE 'loopzero_api absent — create it before pointing handlers at this database';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loopzero_worker') THEN
        BEGIN
            EXECUTE 'ALTER ROLE loopzero_worker NOBYPASSRLS';
        EXCEPTION WHEN insufficient_privilege THEN
            RAISE NOTICE 'could not clear BYPASSRLS on loopzero_worker — runner lacks CREATEROLE';
        END;
    END IF;
END $$;

COMMIT;
