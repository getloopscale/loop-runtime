-- Migration 000003: Deploy support — partial unique index and build_apps table
-- Sprint 2: loopzero deploy happy path
-- Forward-only: no DROP statements

-- Partial unique index: ensures at most one active build (QUEUED or RUNNING)
-- per (tenant, project, environment). See spec §4.2.
CREATE UNIQUE INDEX IF NOT EXISTS idx_one_active_build_per_env
    ON builds (tenant_id, project_id, environment)
    WHERE status IN ('QUEUED', 'RUNNING');

-- build_apps: tracks individual apps within a build
CREATE TABLE IF NOT EXISTS build_apps (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    build_id     TEXT NOT NULL REFERENCES builds(id),
    app_name     TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'PENDING',
    duration_ms  INTEGER,
    error        TEXT,
    image_digest TEXT,
    url          TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(build_id, app_name)
);

-- Add apps JSONB column to releases for storing AppRelease[] data
ALTER TABLE releases ADD COLUMN IF NOT EXISTS apps JSONB;
