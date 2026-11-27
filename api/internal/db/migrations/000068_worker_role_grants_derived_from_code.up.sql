-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Grants loop_runtime_worker the privileges the engine's cross-tenant paths execute
-- on tables created after migration 000023, and nothing else (#385).
--
-- loop_runtime_worker is the one role that may cross tenants under the fail-closed
-- policies of 000062. 000023 granted it an explicit table list; the tables
-- created afterwards were never granted, and no ALTER DEFAULT PRIVILEGES
-- covers the role. Measured on a database built from 000001-000067: 13
-- tables with no privilege and 3 with partial ones, so the bootstrap path
-- (POST /internal/bootstrap, on the cross-tenant pool) and the deploy outbox
-- written inside the background loops' lock transactions were refused with
-- `permission denied`.
--
-- LEAST PRIVILEGE, DERIVED FROM THE CODE. Each grant below is a verb a query
-- actually runs on the cross-tenant pool, and the static test
-- api/internal/db/worker_role_grants_test.go recomputes the set from the query
-- files and the migrations:
--
--   accounts        SELECT, INSERT  bootstrap.sql GetBootstrapAccountIDByOwner,
--                                   InsertBootstrapAccount (PostgresBootstrapStore
--                                   on the cross-tenant pool, cmd/setup_server.go)
--   deploy_events   INSERT          deploy_events.sql InsertDeployEvent, staged
--                                   by release activation and the reconcile /
--                                   reaper deploy.failed emit in the background
--                                   loops' lock transactions
--   deploy_events_id_seq USAGE      the BIGSERIAL default of that INSERT
--   metering_cursor DELETE          tenants.sql AdminEvictMeteringCursors, the
--                                   last step of admin tenant eviction, which
--                                   runs on the cross-tenant pool (#386)
--
-- Deliberately NOT granted: the tables the engine never queries on that pool.
-- preview_slot is read and written on the request pool only; lens_*,
-- memory_*, workspace_webhook*, storage_objects and loop_runtime_api_keys belong to
-- the control plane (loop_console, 000063) and no engine query touches them;
-- metering_config keeps the SELECT 000046 gave it (the metering loop only
-- reads the kill-switch).

GRANT SELECT, INSERT ON accounts TO loop_runtime_worker;
GRANT INSERT ON deploy_events TO loop_runtime_worker;
GRANT USAGE ON SEQUENCE deploy_events_id_seq TO loop_runtime_worker;
GRANT DELETE ON metering_cursor TO loop_runtime_worker;
