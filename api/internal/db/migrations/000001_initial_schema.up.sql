-- LoopRuntime PaaS — Consolidated initial schema.
-- Replaces 37 incremental migrations into a single idempotent schema.
-- Generated from pg_dump of production-equivalent database (2026-04-24).
--
-- This migration is safe to run on empty databases.
-- For existing databases with the old migration history, see UPGRADE.md.

-- Extensions
-- pg_stat_statements is pre-loaded by Postgres via shared_preload_libraries
-- (see infra/helm/postgresql/values.yaml + production DOKS PG configuration).
-- CREATE EXTENSION here would require superuser; the application user `loop_runtime`
-- does NOT have CREATE EXTENSION privilege. Removed in
-- dogfood-2026-05-08-fixes T1.2 (golang-migrate adoption) — legacy psql
-- loop silently swallowed the permission error; golang-migrate marks the
-- migration dirty + aborts. The extension is provisioned at the cluster
-- level, not at the app-schema level.

-- ─────────────────────────────────────────────────────────────────────────────
-- Functions (triggers depend on these)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE OR REPLACE FUNCTION audit_log_prevent_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audit_log is append-only: % not allowed', TG_OP;
  RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION audit_sensitive_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
  v_tenant_id TEXT;
  v_resource_id TEXT;
BEGIN
  IF TG_OP = 'DELETE' THEN
    v_tenant_id := OLD.tenant_id;
  ELSE
    v_tenant_id := NEW.tenant_id;
  END IF;

  IF TG_OP = 'DELETE' THEN
    IF TG_TABLE_NAME = 'secrets' THEN v_resource_id := OLD.id;
    ELSE v_resource_id := OLD.tenant_id || ':' || OLD.user_id;
    END IF;
  ELSE
    IF TG_TABLE_NAME = 'secrets' THEN v_resource_id := NEW.id;
    ELSE v_resource_id := NEW.tenant_id || ':' || NEW.user_id;
    END IF;
  END IF;

  INSERT INTO audit_log (tenant_id, action, actor, resource_type, resource_id, correlation_id, metadata)
  VALUES (
    v_tenant_id, TG_OP,
    COALESCE(current_setting('app.current_user', true), 'system'),
    TG_TABLE_NAME, v_resource_id,
    current_setting('app.correlation_id', true),
    jsonb_build_object('trigger', true, 'table', TG_TABLE_NAME, 'op', TG_OP)
  );

  IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION populate_cell_id_from_tenant() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.cell_id = 'cell-default' OR NEW.cell_id IS NULL THEN
        SELECT cell_id INTO NEW.cell_id FROM tenants WHERE id = NEW.tenant_id;
        IF NEW.cell_id IS NULL THEN NEW.cell_id := 'cell-default'; END IF;
    END IF;
    RETURN NEW;
END;
$$;

-- ─────────────────────────────────────────────────────────────────────────────
-- Tables
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL,
    github_id BIGINT,
    display_name TEXT NOT NULL DEFAULT '',
    username TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tenants (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL DEFAULT '',
    slug TEXT NOT NULL,
    owner_id TEXT NOT NULL REFERENCES users(id),
    cell_id TEXT NOT NULL DEFAULT 'cell-default',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tenant_members (
    tenant_id TEXT NOT NULL REFERENCES tenants(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'admin', 'member')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, user_id)
);

CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id),
    name TEXT NOT NULL,
    created_by TEXT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS builds (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('QUEUED','RUNNING','RELEASING','SUCCEEDED','FAILED','CANCELLING','CANCELLED')),
    correlation_id TEXT NOT NULL,
    created_by TEXT NOT NULL,
    commit_sha TEXT,
    apps_spec JSONB,
    workflow_id TEXT,
    error_message TEXT,
    duration_seconds INTEGER,
    cell_id TEXT NOT NULL DEFAULT 'cell-default',
    processing_by TEXT,
    processing_since TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS build_apps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    build_id TEXT NOT NULL REFERENCES builds(id),
    app_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING',
    duration_ms INTEGER,
    error TEXT,
    image_digest TEXT,
    url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (build_id, app_name)
);

CREATE TABLE IF NOT EXISTS releases (
    id TEXT PRIMARY KEY,
    build_id TEXT NOT NULL REFERENCES builds(id),
    tenant_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('READY','PENDING','ACTIVE','GIT_MIRROR_FAILED','SYNC_FAILED','DRIFT_ALERT','SUPERSEDED','ROLLING_BACK','ROLLED_BACK','ROLLBACK_FAILED','CANCELLED')),
    correlation_id TEXT NOT NULL,
    created_by TEXT NOT NULL,
    git_commit TEXT,
    git_mirror_status TEXT NOT NULL DEFAULT 'pending',
    artifacts JSONB,
    side_effects JSONB,
    apps JSONB,
    cell_id TEXT NOT NULL DEFAULT 'cell-default',
    processing_by TEXT,
    processing_since TIMESTAMPTZ,
    image_digest TEXT,
    image_signature TEXT,
    image_signed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS environment_targets (
    tenant_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment TEXT NOT NULL,
    desired_release_id TEXT NOT NULL REFERENCES releases(id),
    active_release_id TEXT REFERENCES releases(id),
    updated_by TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, project_id, environment)
);

CREATE TABLE IF NOT EXISTS secrets (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    project_id TEXT NOT NULL DEFAULT '',
    environment TEXT NOT NULL DEFAULT '',
    app TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    encrypted_value BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, project_id, environment, app, name)
);

CREATE TABLE IF NOT EXISTS sse_events (
    id BIGSERIAL PRIMARY KEY,
    build_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    data JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS idempotency_keys (
    key TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    endpoint TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    response_status INTEGER NOT NULL,
    response_body JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS circuit_breakers (
    key TEXT PRIMARY KEY,
    state TEXT NOT NULL DEFAULT 'closed' CHECK (state IN ('closed', 'open')),
    failure_count INTEGER NOT NULL DEFAULT 0,
    success_count INTEGER NOT NULL DEFAULT 0,
    last_failure TIMESTAMPTZ,
    last_success TIMESTAMPTZ,
    opened_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS webhook_events (
    event_id TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'processed',
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error TEXT,
    attempt_count INTEGER NOT NULL DEFAULT 1,
    payload_hash TEXT
);

CREATE TABLE IF NOT EXISTS audit_log (
    id SERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    action TEXT NOT NULL,
    actor TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    correlation_id TEXT,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL UNIQUE REFERENCES tenants(id),
    stripe_customer_id TEXT,
    stripe_subscription_id TEXT,
    plan TEXT NOT NULL DEFAULT 'starter' CHECK (plan IN ('starter','pro','team','enterprise')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','past_due','canceled','incomplete','trialing','unpaid')),
    current_period_start TIMESTAMPTZ,
    current_period_end TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS plan_limits (
    plan TEXT NOT NULL,
    metric TEXT NOT NULL,
    hard_limit INTEGER,
    soft_limit INTEGER,
    PRIMARY KEY (plan, metric)
);

CREATE TABLE IF NOT EXISTS usage_records (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id),
    metric TEXT NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 0,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    reported_to_stripe BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_identity_map (
    kratos_identity_id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS custom_domains (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    project_name TEXT NOT NULL,
    app_name TEXT NOT NULL,
    environment TEXT NOT NULL,
    domain TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending_verification'
      CHECK (status IN ('pending_verification','verified','pending_ns','ns_active','provisioning','active','degraded','failed','removing')),
    verification_token TEXT NOT NULL,
    verified_at TIMESTAMPTZ,
    tls_status TEXT DEFAULT 'pending'
      CHECK (tls_status IN ('pending','issuing','issued','failed')),
    tls_expires_at TIMESTAMPTZ,
    failure_reason TEXT,
    last_cname_check TIMESTAMPTZ,
    cname_consecutive_failures INTEGER NOT NULL DEFAULT 0,
    cf_zone_id TEXT,
    cf_zone_status TEXT DEFAULT 'none'
      CHECK (cf_zone_status IN ('none','pending','active','failed','removed')),
    cf_nameservers TEXT[],
    dns_records_imported BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS github_installations (
    id BIGINT PRIMARY KEY,
    account_login TEXT NOT NULL,
    account_type TEXT NOT NULL DEFAULT 'User',
    tenant_id TEXT REFERENCES tenants(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS project_repositories (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_name TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT 'github',
    repo_full_name TEXT NOT NULL UNIQUE,
    repo_id BIGINT,
    installation_id BIGINT NOT NULL REFERENCES github_installations(id),
    default_branch TEXT NOT NULL DEFAULT 'main',
    auto_deploy BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS github_webhook_deliveries (
    delivery_id TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS leader_generations (
    name TEXT NOT NULL,
    cell_id TEXT NOT NULL DEFAULT 'cell-default',
    holder TEXT NOT NULL,
    generation_id BIGINT NOT NULL DEFAULT 1,
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (name, cell_id)
);

CREATE TABLE IF NOT EXISTS work_queue (
    id BIGSERIAL PRIMARY KEY,
    queue TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    claimed_by TEXT,
    claimed_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tenant_tombstones (
    tenant_id TEXT PRIMARY KEY,
    tenant_slug TEXT NOT NULL,
    deleted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    requested_by TEXT NOT NULL,
    reason TEXT NOT NULL,
    resources_cleaned JSONB NOT NULL DEFAULT '{}',
    resources_failed JSONB,
    anonymized_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS tenants_pending_deletion (
    tenant_id TEXT PRIMARY KEY REFERENCES tenants(id),
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    requested_by TEXT NOT NULL,
    reason TEXT NOT NULL,
    grace_until TIMESTAMPTZ,
    force BOOLEAN NOT NULL DEFAULT false,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    last_error_at TIMESTAMPTZ
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Indexes
-- ─────────────────────────────────────────────────────────────────────────────

CREATE INDEX IF NOT EXISTS idx_builds_tenant_project ON builds (tenant_id, project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_builds_cell_status_created ON builds (cell_id, status, created_at);
CREATE INDEX IF NOT EXISTS idx_builds_processing_since ON builds (processing_since) WHERE processing_by IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_one_active_build_per_env ON builds (tenant_id, project_id, environment) WHERE status IN ('QUEUED', 'RUNNING');

CREATE INDEX IF NOT EXISTS idx_releases_env ON releases (tenant_id, project_id, environment, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_releases_build_id ON releases (build_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_releases_pending ON releases (created_at) WHERE status = 'PENDING';
CREATE INDEX IF NOT EXISTS idx_releases_cell_status_created ON releases (cell_id, status, created_at);
CREATE INDEX IF NOT EXISTS idx_releases_processing_since ON releases (processing_since) WHERE processing_by IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_secrets_lookup ON secrets (tenant_id, project_id, environment, app);
CREATE INDEX IF NOT EXISTS idx_secrets_tenant_project_env ON secrets (tenant_id, project_id, environment);
CREATE INDEX IF NOT EXISTS idx_sse_events_build ON sse_events (build_id, id);
CREATE INDEX IF NOT EXISTS idx_idempotency_expires ON idempotency_keys (expires_at);
CREATE INDEX IF NOT EXISTS idx_projects_tenant ON projects (tenant_id);
CREATE INDEX IF NOT EXISTS idx_tenant_members_user ON tenant_members (user_id);
CREATE INDEX IF NOT EXISTS idx_tenants_cell_id ON tenants (cell_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tenants_slug ON tenants (slug);
CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_unique ON users (email);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_github_id_unique ON users (github_id) WHERE github_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username_unique ON users (username);
CREATE INDEX IF NOT EXISTS idx_webhook_events_type ON webhook_events (event_type);
CREATE INDEX IF NOT EXISTS idx_subscriptions_tenant ON subscriptions (tenant_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_stripe_customer ON subscriptions (stripe_customer_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_tenant_metric_period ON usage_records (tenant_id, metric, period_start);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_identity_map_user_id ON user_identity_map (user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_custom_domains_domain ON custom_domains (domain);
CREATE INDEX IF NOT EXISTS idx_custom_domains_tenant_project_app_env ON custom_domains (tenant_id, project_name, app_name, environment);
CREATE INDEX IF NOT EXISTS idx_custom_domains_status ON custom_domains (status) WHERE status IN ('verified', 'provisioning', 'removing');
CREATE INDEX IF NOT EXISTS idx_custom_domains_pending_ns ON custom_domains (cf_zone_status) WHERE status = 'pending_ns';
CREATE INDEX IF NOT EXISTS idx_custom_domains_ns_active ON custom_domains (status) WHERE status = 'ns_active';
CREATE INDEX IF NOT EXISTS idx_custom_domains_active_cname_check ON custom_domains (last_cname_check) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_project_repos_tenant ON project_repositories (tenant_id, project_name);
CREATE INDEX IF NOT EXISTS idx_project_repos_repo ON project_repositories (repo_full_name);
CREATE INDEX IF NOT EXISTS idx_project_repos_installation ON project_repositories (installation_id);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_received ON github_webhook_deliveries (received_at);
CREATE INDEX IF NOT EXISTS idx_leader_generations_heartbeat ON leader_generations (heartbeat_at);
CREATE INDEX IF NOT EXISTS idx_work_queue_dequeue ON work_queue (queue, status, created_at) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_work_queue_stale ON work_queue (status, claimed_at) WHERE status = 'processing';
CREATE INDEX IF NOT EXISTS idx_pending_deletion_grace ON tenants_pending_deletion (grace_until) WHERE grace_until IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tombstones_anonymization ON tenant_tombstones (deleted_at) WHERE anonymized_at IS NULL;

-- ─────────────────────────────────────────────────────────────────────────────
-- Triggers
-- ─────────────────────────────────────────────────────────────────────────────

CREATE CONSTRAINT TRIGGER trg_audit_log_immutable
  AFTER DELETE OR UPDATE ON audit_log
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION audit_log_prevent_mutation();

CREATE TRIGGER trg_builds_populate_cell_id
  BEFORE INSERT ON builds
  FOR EACH ROW EXECUTE FUNCTION populate_cell_id_from_tenant();

CREATE TRIGGER trg_releases_populate_cell_id
  BEFORE INSERT ON releases
  FOR EACH ROW EXECUTE FUNCTION populate_cell_id_from_tenant();

CREATE CONSTRAINT TRIGGER trg_secrets_audit
  AFTER INSERT OR DELETE OR UPDATE ON secrets
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION audit_sensitive_mutation();

CREATE CONSTRAINT TRIGGER trg_tenant_members_audit
  AFTER INSERT OR DELETE OR UPDATE ON tenant_members
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION audit_sensitive_mutation();

-- ─────────────────────────────────────────────────────────────────────────────
-- Row Level Security
-- ─────────────────────────────────────────────────────────────────────────────

ALTER TABLE builds ENABLE ROW LEVEL SECURITY;
CREATE POLICY builds_tenant_isolation ON builds
  USING (tenant_id = current_setting('app.tenant_id', true)
         OR current_setting('app.tenant_id', true) IS NULL
         OR current_setting('app.tenant_id', true) = '');

ALTER TABLE releases ENABLE ROW LEVEL SECURITY;
CREATE POLICY releases_tenant_isolation ON releases
  USING (tenant_id = current_setting('app.tenant_id', true)
         OR current_setting('app.tenant_id', true) IS NULL
         OR current_setting('app.tenant_id', true) = '');

ALTER TABLE environment_targets ENABLE ROW LEVEL SECURITY;
CREATE POLICY environment_targets_tenant_isolation ON environment_targets
  USING (tenant_id = current_setting('app.tenant_id', true)
         OR current_setting('app.tenant_id', true) IS NULL
         OR current_setting('app.tenant_id', true) = '');

ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
CREATE POLICY projects_tenant_isolation ON projects
  USING (tenant_id = current_setting('app.tenant_id', true)
         OR current_setting('app.tenant_id', true) IS NULL
         OR current_setting('app.tenant_id', true) = '');

ALTER TABLE secrets ENABLE ROW LEVEL SECURITY;
CREATE POLICY secrets_tenant_isolation ON secrets
  USING (tenant_id = current_setting('app.tenant_id', true)
         OR current_setting('app.tenant_id', true) IS NULL
         OR current_setting('app.tenant_id', true) = '');

ALTER TABLE idempotency_keys ENABLE ROW LEVEL SECURITY;
CREATE POLICY idempotency_keys_tenant_isolation ON idempotency_keys
  USING (tenant_id = current_setting('app.tenant_id', true)
         OR current_setting('app.tenant_id', true) IS NULL
         OR current_setting('app.tenant_id', true) = '');

ALTER TABLE tenant_members ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_members_tenant_isolation ON tenant_members
  USING (tenant_id = current_setting('app.tenant_id', true)
         OR current_setting('app.tenant_id', true) IS NULL
         OR current_setting('app.tenant_id', true) = '');

ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
CREATE POLICY audit_log_tenant_isolation ON audit_log
  USING (tenant_id = current_setting('app.tenant_id', true)
         OR current_setting('app.tenant_id', true) IS NULL
         OR current_setting('app.tenant_id', true) = '');
