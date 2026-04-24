-- Migration 000027: GitHub App integration tables
-- Stores GitHub App installations and project↔repository links.
-- Enables auto-deploy on push (model Vercel).
-- Forward-only migration.

CREATE TABLE IF NOT EXISTS github_installations (
    id              BIGINT PRIMARY KEY,
    account_login   TEXT NOT NULL,
    account_type    TEXT NOT NULL DEFAULT 'User',
    tenant_id       TEXT REFERENCES tenants(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS project_repositories (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_name    TEXT NOT NULL,
    provider        TEXT NOT NULL DEFAULT 'github',
    repo_full_name  TEXT NOT NULL,
    repo_id         BIGINT,
    installation_id BIGINT NOT NULL REFERENCES github_installations(id),
    default_branch  TEXT NOT NULL DEFAULT 'main',
    auto_deploy     BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(repo_full_name)
);

CREATE INDEX IF NOT EXISTS idx_project_repos_tenant
    ON project_repositories(tenant_id, project_name);
CREATE INDEX IF NOT EXISTS idx_project_repos_installation
    ON project_repositories(installation_id);
CREATE INDEX IF NOT EXISTS idx_project_repos_repo
    ON project_repositories(repo_full_name);

COMMENT ON TABLE github_installations IS 'GitHub App installations — one per org/user that installed the app';
COMMENT ON TABLE project_repositories IS 'Links LoopZero projects to GitHub repositories for auto-deploy';
COMMENT ON COLUMN project_repositories.auto_deploy IS 'When true, pushes to default_branch trigger production deploys';
