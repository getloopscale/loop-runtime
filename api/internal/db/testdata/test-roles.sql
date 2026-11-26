-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Roles for an integration-test database.
--
-- Reproduces the production role split so the isolation tests measure the
-- real posture instead of a convenient one. Run as a superuser BEFORE the
-- schema, then apply the schema as `loop_runtime` so that role owns the tables.
--
--   psql -U postgres -d loop_runtime_test -f api/internal/db/testdata/test-roles.sql
--   psql -U loop_runtime     -d loop_runtime_test -f api/internal/db/schema.gen.sql
--   psql -U loop_runtime     -d loop_runtime_test -f api/internal/db/migrations/000062_rls_fail_closed.up.sql
--
-- Passwords are fixed and worthless on purpose — this file provisions a
-- throwaway test database and must never be pointed at a real one.

DO $$
BEGIN
    -- Owner: migrations and admin scripts. Never serves a request.
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loop_runtime') THEN
        CREATE ROLE loop_runtime LOGIN PASSWORD 'loop_runtime' NOSUPERUSER NOBYPASSRLS CREATEROLE;
    END IF;

    -- Request path. Non-owner and non-bypassing: the policies must bind here,
    -- and the isolation tests fail loudly if this role can escape them.
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loop_runtime_api') THEN
        CREATE ROLE loop_runtime_api LOGIN PASSWORD 'loop-runtime-api' NOSUPERUSER NOBYPASSRLS;
    END IF;

    -- Background loops. Reaches every tenant through the named
    -- *_worker_cross_tenant policies, never through BYPASSRLS.
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loop_runtime_worker') THEN
        CREATE ROLE loop_runtime_worker LOGIN PASSWORD 'loop-runtime-worker' NOSUPERUSER NOBYPASSRLS;
    END IF;
END $$;

GRANT CREATE, USAGE ON SCHEMA public TO loop_runtime;
GRANT USAGE ON SCHEMA public TO loop_runtime_api, loop_runtime_worker;

-- Table-level grants are applied by migration 000062 for loop_runtime_api and by
-- migration 000023 for loop_runtime_worker. The defaults below cover tables created
-- by the schema dump before those migrations run.
ALTER DEFAULT PRIVILEGES FOR ROLE loop_runtime IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO loop_runtime_api, loop_runtime_worker;
ALTER DEFAULT PRIVILEGES FOR ROLE loop_runtime IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO loop_runtime_api, loop_runtime_worker;
