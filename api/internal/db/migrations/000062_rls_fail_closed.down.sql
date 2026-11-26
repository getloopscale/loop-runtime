-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Rollback of 000062. Drops the isolation policies and releases FORCE.
--
-- READ THIS BEFORE RUNNING IT: applying this down-migration restores the
-- state measured on 2026-08-14, in which a connection scoped to one tenant
-- reads every other tenant's rows with no error and no log line. It exists
-- so an operator has a documented exit if the fail-closed policies break a
-- code path in production — not because reverting is ever routine.
--
-- If a query started returning zero rows after 000062, the cause is almost
-- always a caller that never set `app.tenant_id`. The fix is to set it in
-- that caller, or to run the workload as `loop_runtime_worker` (which the
-- cross-tenant policy covers). Reverting hides the bug and reopens the
-- exposure for every other caller at the same time.

BEGIN;

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
        'tenant_tombstones', 'tenants_pending_deletion', 'loop_runtime_api_keys',
        'usage_records', 'workspace_webhook_deliveries', 'workspace_webhooks'
    ];
BEGIN
    FOREACH t IN ARRAY tenant_tables LOOP
        IF NOT EXISTS (
            SELECT 1 FROM information_schema.tables
            WHERE table_schema = 'public' AND table_name = t
        ) THEN
            CONTINUE;
        END IF;

        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', t || '_tenant_isolation', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', t || '_worker_cross_tenant', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', t || '_owner_maintenance', t);
        EXECUTE format('ALTER TABLE public.%I NO FORCE ROW LEVEL SECURITY', t);
    END LOOP;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'tenants'
    ) THEN
        DROP POLICY IF EXISTS tenants_self_isolation ON public.tenants;
        DROP POLICY IF EXISTS tenants_worker_cross_tenant ON public.tenants;
        DROP POLICY IF EXISTS tenants_owner_maintenance ON public.tenants;
        ALTER TABLE public.tenants NO FORCE ROW LEVEL SECURITY;
        ALTER TABLE public.tenants DISABLE ROW LEVEL SECURITY;
    END IF;
END $$;

COMMIT;
