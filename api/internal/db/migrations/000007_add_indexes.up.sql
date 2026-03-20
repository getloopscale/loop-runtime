-- Migration 000007: Add covering indexes for common query patterns
-- Forward-only: no DROP statements
--
-- Index strategy:
--   builds    — queried by (tenant, project) for listing, already has partial unique
--               index idx_one_active_build_per_env for concurrent build checks
--   releases  — queried by (tenant, project, environment) for release listing/activation
--   env_targets — PK is (tenant, project, environment), no extra index needed
--   sse_events  — already indexed by (build_id, id) as idx_sse_events_build
--   idempotency — needs expires_at index for TTL cleanup
--   secrets     — already has idx_secrets_tenant_project_env, needs app-level lookup

-- Listing builds by tenant+project, ordered by creation time (descending)
CREATE INDEX IF NOT EXISTS idx_builds_tenant_project
    ON builds (tenant_id, project_id, created_at DESC);

-- Listing releases by environment, ordered by creation time (descending)
CREATE INDEX IF NOT EXISTS idx_releases_env
    ON releases (tenant_id, project_id, environment, created_at DESC);

-- TTL cleanup of expired idempotency keys (used by background reaper)
CREATE INDEX IF NOT EXISTS idx_idempotency_expires
    ON idempotency_keys (expires_at);

-- Secret lookup by app (extends existing idx_secrets_tenant_project_env)
CREATE INDEX IF NOT EXISTS idx_secrets_lookup
    ON secrets (tenant_id, project_id, environment, app);
