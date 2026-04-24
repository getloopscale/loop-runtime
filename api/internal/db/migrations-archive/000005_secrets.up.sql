-- Migration 000005: Secrets metadata table
-- Sprint 3: secret management (spec §3)
-- Forward-only: no DROP statements
--
-- Stores encrypted secret values per (tenant, project, environment, app).
-- The API only exposes metadata (name, environment, app) — NEVER values.

CREATE TABLE IF NOT EXISTS secrets (
    id               TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL,
    project_id       TEXT NOT NULL DEFAULT '',
    environment      TEXT NOT NULL DEFAULT '',
    app              TEXT NOT NULL DEFAULT '',
    name             TEXT NOT NULL,
    encrypted_value  BYTEA NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, project_id, environment, app, name)
);

CREATE INDEX IF NOT EXISTS idx_secrets_tenant_project_env
    ON secrets(tenant_id, project_id, environment);
