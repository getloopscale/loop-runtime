-- ============================================================================
-- 000004_preview_environments
--
-- Sprint 5 / T2.1 — Preview Environments dual-write store (ADR D7).
-- Note: 000003 was reserved for a "deploy_support" migration that never
-- landed; the slot stays skipped to avoid a number collision.
--
-- Stores preview environment metadata for API queries (list/get/audit).
-- Operational state lives in the PreviewEnvironment CR; this table is the
-- source of truth for queries reachable from the developer plane (CLI/API)
-- and provides history beyond the CR lifecycle.
--
-- Forward-only migration (no DOWN file) per LoopZero conventions.
-- ============================================================================

CREATE TABLE IF NOT EXISTS preview_environments (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id      TEXT NOT NULL,
    pr_number       INTEGER NOT NULL CHECK (pr_number > 0),
    branch          TEXT NOT NULL,
    commit_sha      TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'creating'
                    CHECK (status IN (
                        'creating',
                        'active',
                        'expiring',
                        'deleting',
                        'deleted',
                        'failed',
                        'cleanup_failed'
                    )),
    url             TEXT,
    build_id        TEXT REFERENCES builds(id),
    release_id      TEXT REFERENCES releases(id),
    repo_full_name  TEXT,
    installation_id BIGINT,
    expires_at      TIMESTAMPTZ NOT NULL,
    cell_id         TEXT NOT NULL DEFAULT 'cell-default',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ,

    -- Invariant from plan: at most one live preview per (tenant, project, PR).
    -- Soft-deleted rows are excluded via partial unique index below to allow
    -- a closed PR's row to coexist with a future preview if the PR is
    -- reopened later.
    CONSTRAINT preview_environments_id_format CHECK (id LIKE 'prev-%')
);

-- One ACTIVE preview per (tenant, project, pr_number).
-- Soft-deleted rows do not block creation of a new preview for the same PR.
CREATE UNIQUE INDEX IF NOT EXISTS uniq_preview_active_per_pr
    ON preview_environments (tenant_id, project_id, pr_number)
    WHERE deleted_at IS NULL;

-- Common query paths.
CREATE INDEX IF NOT EXISTS idx_preview_tenant_status
    ON preview_environments (tenant_id, status)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_preview_expires
    ON preview_environments (expires_at)
    WHERE deleted_at IS NULL AND status NOT IN ('deleted', 'deleting');

CREATE INDEX IF NOT EXISTS idx_preview_build
    ON preview_environments (build_id)
    WHERE build_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_preview_cell_status_created
    ON preview_environments (cell_id, status, created_at);

-- Row-level security follows the pattern used by builds/releases:
-- when app.tenant_id is set, restrict to that tenant; when unset, allow
-- (admin contexts and migrations bypass RLS via SET LOCAL or unset GUC).
ALTER TABLE preview_environments ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS preview_environments_tenant_isolation ON preview_environments;
CREATE POLICY preview_environments_tenant_isolation
    ON preview_environments
    USING (
        tenant_id = current_setting('app.tenant_id', true)
        OR current_setting('app.tenant_id', true) IS NULL
        OR current_setting('app.tenant_id', true) = ''
    );

-- Auto-fill cell_id from tenant on insert (matches builds/releases pattern).
DROP TRIGGER IF EXISTS trg_preview_populate_cell_id ON preview_environments;
CREATE TRIGGER trg_preview_populate_cell_id
    BEFORE INSERT ON preview_environments
    FOR EACH ROW EXECUTE FUNCTION populate_cell_id_from_tenant();

-- Extend audit_sensitive_mutation to handle the new preview_environments table.
-- The base function (000001) routes resource_id by table: secrets uses NEW.id,
-- tenant_members uses tenant_id || ':' || user_id. preview_environments has
-- no user_id, so we use NEW.id (the preview ID) — same shape as secrets.
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
    IF TG_TABLE_NAME IN ('secrets', 'preview_environments') THEN
      v_resource_id := OLD.id;
    ELSE
      v_resource_id := OLD.tenant_id || ':' || OLD.user_id;
    END IF;
  ELSE
    IF TG_TABLE_NAME IN ('secrets', 'preview_environments') THEN
      v_resource_id := NEW.id;
    ELSE
      v_resource_id := NEW.tenant_id || ':' || NEW.user_id;
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

-- Audit trigger — every mutation is recorded.
DROP TRIGGER IF EXISTS trg_preview_environments_audit ON preview_environments;
CREATE CONSTRAINT TRIGGER trg_preview_environments_audit
    AFTER INSERT OR DELETE OR UPDATE ON preview_environments
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION audit_sensitive_mutation();

-- Maintain updated_at on every UPDATE.
CREATE OR REPLACE FUNCTION preview_environments_set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_preview_environments_updated_at ON preview_environments;
CREATE TRIGGER trg_preview_environments_updated_at
    BEFORE UPDATE ON preview_environments
    FOR EACH ROW EXECUTE FUNCTION preview_environments_set_updated_at();

COMMENT ON TABLE preview_environments IS
    'Preview environment metadata for API queries (Sprint 5, ADR D7). '
    'Operational state lives in the PreviewEnvironment CR.';

COMMENT ON COLUMN preview_environments.status IS
    'Lifecycle phase mirroring the state machine in pkg/statemachine/preview.go '
    '(stored lowercase here; Phase casing in the CR is "Creating"/"Active"/etc.).';

COMMENT ON INDEX uniq_preview_active_per_pr IS
    'Enforces one live preview per (tenant, project, PR). Soft-deleted rows '
    '(deleted_at IS NOT NULL) are excluded so a reopened PR can re-create a preview.';
