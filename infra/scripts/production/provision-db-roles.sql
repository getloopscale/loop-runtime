-- Provision the non-owner roles loopzero-api (#396) and loopzero-cloud (#438) connect as.
--
-- loopzero-cloud refuses to serve as a role holding an unnamed cross-tenant privilege
-- (superuser, BYPASSRLS, table owner), so it connects as `loopzero_cloud`. Migration
-- 000063 creates that role WITH PASSWORD NULL and gives it its named policies;
-- creating it here first, with a password, is what lets anything log in as it.
--
-- loopzero-api refuses to serve requests over a pool that can bypass RLS
-- (db.AssertRequestPoolIsolated), so its request pool connects as `loopzero_api` and
-- its cross-tenant background loops as `loopzero_worker`. No migration creates
-- `loopzero_api` (000062 only grants to it when it exists), and 000023 creates
-- `loopzero_worker` WITH PASSWORD NULL — so on an in-cluster Postgres nothing could
-- log in as either. A managed database gets them from its provider instead.
--
-- Runs as the Postgres superuser, BEFORE the migrations: 000062 and 000068 grant
-- to these roles only if they already exist when they run.
--
-- Idempotent. Passwords arrive as psql variables (`-v api_password=…`), never on a
-- command line, and are quoted by format(%L).
--
-- Invoked by `task db:roles`.

\set ON_ERROR_STOP on

SELECT format('CREATE ROLE loopzero_api LOGIN PASSWORD %L', :'api_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loopzero_api') \gexec

SELECT format('CREATE ROLE loopzero_worker LOGIN PASSWORD %L', :'worker_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loopzero_worker') \gexec

-- Re-asserted on every run, so a role created earlier by hand or by a test
-- fixture converges to the posture the API checks at boot.
SELECT format('CREATE ROLE loopzero_cloud LOGIN PASSWORD %L', :'cloud_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'loopzero_cloud') \gexec
SELECT format('ALTER ROLE loopzero_api LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD %L', :'api_password') \gexec
SELECT format('ALTER ROLE loopzero_worker LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD %L', :'worker_password') \gexec

SELECT format('ALTER ROLE loopzero_cloud LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD %L', :'cloud_password') \gexec
SELECT format('GRANT CONNECT ON DATABASE %I TO loopzero_api, loopzero_worker, loopzero_cloud', current_database()) \gexec
