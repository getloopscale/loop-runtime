-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- T5.1 of plan 2026-06-01-p3-infra-observability-test-debt-plan.md v1.1
-- (originally 000023 in plan M1; renumbered to 000022 because plan T3.1
-- worker-role migration has not yet shipped and the partial unique
-- index does not depend on worker-role GRANTs).
--
-- ADR D4: Add a partial unique index on (tenant_id, project_id,
-- environment) WHERE status='ACTIVE' so the at-most-one-ACTIVE
-- invariant is enforced at the DB layer, not only by the app-layer
-- state machine. Closes finding `infra-007` of the deep review.
--
-- BEFORE this migration:
--   * `idx_releases_env` BTREE on (tenant_id, project_id, environment,
--     created_at DESC) — supports range queries but NOT uniqueness.
--   * App-layer state machine forbids two ACTIVE rows via transitions,
--     but a race in build_orchestrator + a bug in the transition
--     handler could silently produce two ACTIVE rows for the same env.
--
-- AFTER:
--   * Any attempt to leave two ACTIVE rows for the same env fails
--     with SQLSTATE 23505 ("duplicate key violates ...").
--   * Routes map this to HTTP 409 CONCURRENT_ACTIVATION (separate code
--     change in routes/releases.go).
--   * `GetActiveByEnv` query becomes an O(1) index lookup instead of
--     a range scan + filter.
--
-- M2 DECISION (plan): plain `CREATE UNIQUE INDEX` (NOT CONCURRENTLY)
-- because (a) golang-migrate wraps statements in a transaction;
-- (b) LoopRuntime is pre-release with <100 releases per env; (c) production
-- at scale can split into a CONCURRENTLY migration later (out of
-- this plan's scope).
--
-- M3 DECISION (plan): no .down.sql per CLAUDE.md forward-only
-- pre-release governance. The conceptual rollback would be:
--     DROP INDEX IF EXISTS idx_releases_active_unique;
-- which can be applied manually by an operator on an emergency
-- basis if the partial-unique invariant ever needs to be relaxed.
--
-- PRE-FLIGHT (operator MUST run before applying in environments
-- with existing data):
--     SELECT tenant_id, project_id, environment, COUNT(*) AS n
--     FROM releases
--     WHERE status = 'ACTIVE'
--     GROUP BY 1, 2, 3
--     HAVING COUNT(*) > 1;
-- If this returns ANY rows, the migration WILL fail. Resolve by
-- choosing one row per group as canonical ACTIVE (typically the
-- newest by created_at) and demoting the others to SUPERSEDED.

CREATE UNIQUE INDEX IF NOT EXISTS idx_releases_active_unique
    ON releases (tenant_id, project_id, environment)
    WHERE status = 'ACTIVE';

-- Index name `idx_releases_active_unique` reused by the application
-- layer for any future EXPLAIN-plan assertions; do not rename without
-- a full grep audit of routes/releases.go + services/*.
