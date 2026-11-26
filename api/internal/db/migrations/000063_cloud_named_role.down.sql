-- Rollback of 000063 — removes the control plane's NAMED cross-tenant policies.
--
-- READ THIS FIRST: this does not reduce the control plane's reach, it hides it
-- again. After this runs, `loop_console` can no longer read across tenants, so the
-- deployment must go back to connecting as the OWNER — which reads every tenant
-- anyway AND holds DDL over the policies meant to constrain it. That is strictly
-- worse, and db.AssertControlPlaneRole will refuse to boot on it.
--
-- The role itself is NOT dropped: dropping a role that owns grants elsewhere
-- fails, and leaving it in place costs nothing.

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
        'usage_records', 'workspace_webhook_deliveries', 'workspace_webhooks',
        'tenants'
    ];
BEGIN
    FOREACH t IN ARRAY tenant_tables LOOP
        IF EXISTS (
            SELECT 1 FROM information_schema.tables
            WHERE table_schema = 'public' AND table_name = t
        ) THEN
            EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', t || '_cloud_cross_tenant', t);
        END IF;
    END LOOP;
END $$;

COMMIT;
