-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- T3.1 of plan 2026-06-01-p3-infra-observability-test-debt-plan.md v1.1
-- (originally 000022 in plan M1; renumbered to 000023 because plan T5.1
-- partial unique index migration shipped first at 000022).
--
-- ADR D3: Create a non-owner PG role `loopruntime_worker` for background loops
-- (reconcile, convergence, reapers, idempotency cleaner, audit pruner,
-- event pruner, build GC, cell metrics collector, etc.). The owner
-- role `loopruntime` continues to run migrations + admin scripts only.
--
-- Why: migration 000010 disabled `FORCE ROW LEVEL SECURITY` because
-- owner-role queries bypass RLS by design (PG documented behavior).
-- The bypass was an emergency fix for the audit_log INSERTs that
-- happen inside reconcile loops + reapers — they have no caller-set
-- tenant_id, so the RLS predicate `tenant_id = current_setting(...)`
-- returned no rows AND no exception (FORCE RLS would have erred).
-- Splitting into a worker role restores RLS for those workloads:
-- the worker is a non-owner, so RLS policies APPLY; queries that
-- need cross-tenant visibility (e.g. reaper scanning all stale
-- builds) explicitly SET app.tenant_id = NULL inside the tx.
--
-- BEFORE this migration:
--   * Single `loopruntime` role used for migrations + handlers + background loops.
--   * `FORCE RLS` disabled (migration 000010) because owner bypasses anyway.
--   * Background queries running as owner silently bypass RLS — defense-
--     in-depth gap (infra-006 MEDIUM finding).
--
-- AFTER:
--   * `loopruntime` owner: migrations + admin scripts ONLY.
--   * `loopruntime_worker` non-owner: background loops + reapers + reconcilers.
--     Receives explicit GRANT on every table referenced by background code.
--   * HTTP request handlers continue to use `loopruntime` for the moment;
--     splitting them off is a follow-up (handlers already SET app.tenant_id
--     per request via middleware so RLS DOES apply even with owner bypass
--     for tenant-scoped queries; the gap was the BACKGROUND code path).
--
-- M3 per CLAUDE.md forward-only governance: no .down.sql. Conceptual
-- rollback (operator emergency only):
--     REVOKE ALL ON ALL TABLES IN SCHEMA public FROM loopruntime_worker;
--     DROP ROLE loopruntime_worker;
--
-- M1 + EC pre-flight:
--   * This migration MUST run as the `loopruntime` owner role. If accidentally
--     applied as worker, the CREATE ROLE statement fails with permission
--     denied — safe failure mode.
--   * Idempotent: every statement uses IF NOT EXISTS / IF EXISTS so
--     repeated apply is safe.
--   * Password placeholder: the role is created WITH PASSWORD NULL.
--     Production sets the actual password via ESO Secret reconciliation
--     OUT OF migration scope (operator workstream — credential rotation
--     lives in the secret store, not the DDL).

-- Defensive guard: assert we're running as the owner.
DO $$
BEGIN
    IF current_user <> 'loopruntime' THEN
        RAISE EXCEPTION 'migration 000023 must run as loopruntime owner; current_user=%', current_user;
    END IF;
END$$;

-- Create the worker role. Initially with no password (operator sets via
-- ALTER ROLE in production deploy script per ESO Secret).
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loopruntime_worker') THEN
        CREATE ROLE loopruntime_worker WITH LOGIN PASSWORD NULL;
    END IF;
END$$;

-- Grant database-level CONNECT so the worker can establish a connection.
-- current_database(), not a literal: replaying this migration against any
-- database not named `loopruntime` (CI, an audit replay) failed here.
DO $$ BEGIN
    EXECUTE format('GRANT CONNECT ON DATABASE %I TO loopruntime_worker', current_database());
END$$;
GRANT USAGE ON SCHEMA public TO loopruntime_worker;

-- Grant per-table privileges. Explicit GRANTs (not ALL TABLES IN SCHEMA)
-- so future operator-only tables (e.g., feature_flags, secret rotation
-- audit) can be opt-in EXCLUDED from the worker role by simply omitting
-- the GRANT — safer-by-default.
--
-- Table list mirrors `\dt` from a healthy PG (audited 2026-06-02). Adding
-- a new table requires extending this migration in a follow-up that
-- GRANTs to loopruntime_worker if the background loops need to touch it; the
-- new pkg/quality gate `TestWorkerRole_GrantsInvariant_AllTablesAccessible`
-- (S1 SHOULD TEST) catches the omission at CI time.
GRANT SELECT, INSERT, UPDATE, DELETE ON
    app_scaling_config,
    audit_log,
    build_apps,
    builds,
    cells,
    circuit_breakers,
    cost_counters,
    custom_domains,
    environment_targets,
    github_installations,
    github_webhook_deliveries,
    idempotency_keys,
    leader_generations,
    plan_limits,
    preview_environments,
    project_repositories,
    projects,
    release_app_attestations,
    releases,
    secrets,
    sse_events,
    subscriptions,
    tenant_members,
    tenant_tombstones,
    tenants,
    tenants_pending_deletion,
    usage_records,
    user_identity_map,
    users,
    webhook_events,
    work_queue
TO loopruntime_worker;

-- Sequences too — INSERTs on tables with SERIAL columns need USAGE on
-- the underlying sequence.
GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO loopruntime_worker;

-- Explicitly NOT GRANTed (operator-only tables):
--   * schema_migrations — migration runner only

-- RLS policies: the worker role is non-owner, so existing policies on
-- public.* tables APPLY to its queries. No new policies needed for the
-- isolation invariant; the existing policy referencing app.tenant_id
-- gates the worker's access exactly like a tenant API user.
--
-- Background workers that need cross-tenant visibility (e.g. reaper
-- scanning all stale builds across tenants) MUST explicitly set
-- app.tenant_id to NULL (or empty string) INSIDE the transaction so
-- the policy's permissive branch fires:
--     SET LOCAL app.tenant_id = '';
-- Per existing policy text (e.g. on releases):
--     USING ((tenant_id = current_setting('app.tenant_id', true))
--         OR (current_setting('app.tenant_id', true) IS NULL)
--         OR (current_setting('app.tenant_id', true) = ''))
-- The empty-string branch matches background-worker scans. This is the
-- M2 design called out in the plan's D3 deep dive.

-- LIVE-validation reference (for the integration test):
--     SET ROLE loopruntime_worker;
--     SET app.tenant_id = 'tenant-A';
--     SELECT id, tenant_id FROM releases LIMIT 100;
--     -- Returns only tenant-A's releases (RLS active).
--     SET app.tenant_id = '';
--     SELECT id, tenant_id FROM releases LIMIT 100;
--     -- Returns all tenants (cross-tenant scan path for reapers).
