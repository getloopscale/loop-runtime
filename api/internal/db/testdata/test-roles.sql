-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Roles for an integration-test database.
--
-- Reproduces the production role split so the isolation tests measure the
-- real posture instead of a convenient one. Run as a superuser BEFORE the
-- schema, then apply the schema as `loopruntime` so that role owns the tables.
--
--   psql -U postgres -d loopruntime_test -f api/internal/db/testdata/test-roles.sql
--   psql -U loopruntime     -d loopruntime_test -f api/internal/db/schema.gen.sql
--   psql -U loopruntime     -d loopruntime_test -f api/internal/db/migrations/000062_rls_fail_closed.up.sql
--
-- Passwords are fixed and worthless on purpose — this file provisions a
-- throwaway test database and must never be pointed at a real one.

DO $$
BEGIN
    -- Owner: migrations and admin scripts. Never serves a request.
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loopruntime') THEN
        CREATE ROLE loopruntime LOGIN PASSWORD 'loopruntime' NOSUPERUSER NOBYPASSRLS CREATEROLE;
    END IF;

    -- Request path. Non-owner and non-bypassing: the policies must bind here,
    -- and the isolation tests fail loudly if this role can escape them.
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loopruntime_api') THEN
        CREATE ROLE loopruntime_api LOGIN PASSWORD 'loopruntime-api' NOSUPERUSER NOBYPASSRLS;
    END IF;

    -- Background loops. Reaches every tenant through the named
    -- *_worker_cross_tenant policies, never through BYPASSRLS.
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loopruntime_worker') THEN
        CREATE ROLE loopruntime_worker LOGIN PASSWORD 'loopruntime-worker' NOSUPERUSER NOBYPASSRLS;
    END IF;
END $$;

GRANT CREATE, USAGE ON SCHEMA public TO loopruntime;
GRANT USAGE ON SCHEMA public TO loopruntime_api, loopruntime_worker;

-- Table-level grants are applied by migration 000062 for loopruntime_api and by
-- migration 000023 for loopruntime_worker. The defaults below cover tables created
-- by the schema dump before those migrations run.
ALTER DEFAULT PRIVILEGES FOR ROLE loopruntime IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO loopruntime_api, loopruntime_worker;
ALTER DEFAULT PRIVILEGES FOR ROLE loopruntime IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO loopruntime_api, loopruntime_worker;
