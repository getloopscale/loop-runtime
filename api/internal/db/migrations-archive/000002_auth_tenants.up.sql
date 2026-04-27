-- Migration 000002: Authentication and multi-tenancy tables
-- Sprint 1: loopzero login + loopzero init + TenantController
-- Forward-only: no DROP statements

-- Users: represents GitHub-authenticated developers
CREATE TABLE IF NOT EXISTS users (
    id          TEXT PRIMARY KEY,                     -- github-{username}
    email       TEXT NOT NULL,
    github_id   BIGINT NOT NULL UNIQUE,
    display_name TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);
CREATE INDEX IF NOT EXISTS idx_users_github_id ON users (github_id);

-- Tenants: organizational unit (team/account)
CREATE TABLE IF NOT EXISTS tenants (
    id           TEXT PRIMARY KEY,                    -- slug (e.g., "acme")
    display_name TEXT NOT NULL DEFAULT '',
    plan         TEXT NOT NULL DEFAULT 'starter',     -- starter, growth, enterprise
    owner_id     TEXT NOT NULL REFERENCES users (id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Tenant members: maps users to tenants with roles
CREATE TABLE IF NOT EXISTS tenant_members (
    tenant_id  TEXT NOT NULL REFERENCES tenants (id),
    user_id    TEXT NOT NULL REFERENCES users (id),
    role       TEXT NOT NULL DEFAULT 'member',        -- owner, admin, member
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_tenant_members_user ON tenant_members (user_id);

-- Projects: a deployable project within a tenant
CREATE TABLE IF NOT EXISTS projects (
    id         TEXT PRIMARY KEY,                       -- proj-{8hex}
    tenant_id  TEXT NOT NULL REFERENCES tenants (id),
    name       TEXT NOT NULL,
    created_by TEXT NOT NULL REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_projects_tenant ON projects (tenant_id);
