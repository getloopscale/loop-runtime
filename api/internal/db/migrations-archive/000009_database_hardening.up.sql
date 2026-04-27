-- Migration 000009: Database Hardening (MEET-2026-03-31-024 Phase 1)
-- Forward-only: no DROP TABLE/COLUMN statements
--
-- This migration implements:
-- 1. CHECK constraints on status columns (DB-01)
-- 2. Missing indexes (DB-02, DB-03)
-- 3. Drop redundant index (DB-05)
-- 4. Role separation (C1): loopzero_api, loopzero_migrate, loopzero_readonly
-- 5. Row-Level Security on ALL tenant-scoped tables (C2)
-- 6. Deferred audit triggers (A.14)

-- ============================================================
-- 1. CHECK CONSTRAINTS ON STATUS COLUMNS (DB-01)
-- Uses NOT VALID + VALIDATE pattern for online-safe application.
-- NOT VALID acquires SHARE UPDATE EXCLUSIVE (allows concurrent DML).
-- VALIDATE scans existing rows without blocking writes.
-- ============================================================

DO $$ BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'chk_builds_status'
  ) THEN
    ALTER TABLE builds ADD CONSTRAINT chk_builds_status
      CHECK (status IN ('QUEUED', 'RUNNING', 'RELEASING', 'SUCCEEDED', 'FAILED', 'CANCELLED'))
      NOT VALID;
  END IF;
END $$;
ALTER TABLE builds VALIDATE CONSTRAINT chk_builds_status;

DO $$ BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'chk_releases_status'
  ) THEN
    ALTER TABLE releases ADD CONSTRAINT chk_releases_status
      CHECK (status IN ('READY', 'PENDING', 'ACTIVE', 'SUPERSEDED', 'ROLLED_BACK', 'CANCELLED'))
      NOT VALID;
  END IF;
END $$;
ALTER TABLE releases VALIDATE CONSTRAINT chk_releases_status;


-- ============================================================
-- 2. MISSING INDEXES (DB-02, DB-03)
-- ============================================================

-- DB-02: Missing index on releases.build_id (FK column, used by GetLatestByBuildID)
CREATE INDEX IF NOT EXISTS idx_releases_build_id
    ON releases (build_id, created_at DESC);

-- DB-03: Missing partial index for PENDING releases (used by convergence loop every 30s)
CREATE INDEX IF NOT EXISTS idx_releases_pending
    ON releases (created_at ASC) WHERE status = 'PENDING';


-- ============================================================
-- 3. DROP REDUNDANT INDEX (DB-05)
-- idempotency_keys.key is already the PRIMARY KEY.
-- No query filters on (key, tenant_id) — the PK covers key lookups.
-- ============================================================

DROP INDEX IF EXISTS idempotency_keys_key_tenant_idx;


-- ============================================================
-- 4. ROLE SEPARATION (C1)
-- Three roles: loopzero_api (DML), loopzero_migrate (DDL), loopzero_readonly (SELECT)
-- The API connects as loopzero_api. Migration runner uses admin.
-- ============================================================

-- Role creation requires CREATEROLE privilege on the executing user.
-- In local dev (k3d), the default 'loopzero' user may lack this privilege.
-- Wrap in a block that gracefully skips when CREATEROLE is unavailable.
-- Production: Pulumi provisions roles via the admin user (has CREATEROLE).
DO $$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'loopzero_api') THEN
    BEGIN
      CREATE ROLE loopzero_api LOGIN PASSWORD 'loopzero-api-change-me'
        NOSUPERUSER NOCREATEDB NOCREATEROLE;
    EXCEPTION WHEN insufficient_privilege THEN
      RAISE NOTICE 'Skipping loopzero_api role creation — current user lacks CREATEROLE (expected in local dev)';
    END;
  END IF;
END $$;

DO $$ BEGIN
  PERFORM 1 FROM pg_roles WHERE rolname = 'loopzero_api';
  IF FOUND THEN
    GRANT CONNECT ON DATABASE loopzero TO loopzero_api;
    GRANT USAGE ON SCHEMA public TO loopzero_api;
    GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO loopzero_api;
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO loopzero_api;
    GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO loopzero_api;
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE ON SEQUENCES TO loopzero_api;
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'loopzero_migrate') THEN
    BEGIN
      CREATE ROLE loopzero_migrate LOGIN PASSWORD 'loopzero-migrate-change-me'
        NOSUPERUSER NOCREATEROLE;
    EXCEPTION WHEN insufficient_privilege THEN
      RAISE NOTICE 'Skipping loopzero_migrate role creation — current user lacks CREATEROLE (expected in local dev)';
    END;
  END IF;
END $$;

DO $$ BEGIN
  PERFORM 1 FROM pg_roles WHERE rolname = 'loopzero_migrate';
  IF FOUND THEN
    GRANT CONNECT ON DATABASE loopzero TO loopzero_migrate;
    GRANT ALL ON SCHEMA public TO loopzero_migrate;
    GRANT ALL ON ALL TABLES IN SCHEMA public TO loopzero_migrate;
    GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO loopzero_migrate;
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO loopzero_migrate;
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO loopzero_migrate;
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'loopzero_readonly') THEN
    BEGIN
      CREATE ROLE loopzero_readonly LOGIN PASSWORD 'loopzero-readonly-change-me'
        NOSUPERUSER NOCREATEDB NOCREATEROLE;
    EXCEPTION WHEN insufficient_privilege THEN
      RAISE NOTICE 'Skipping loopzero_readonly role creation — current user lacks CREATEROLE (expected in local dev)';
    END;
  END IF;
END $$;

DO $$ BEGIN
  PERFORM 1 FROM pg_roles WHERE rolname = 'loopzero_readonly';
  IF FOUND THEN
    GRANT CONNECT ON DATABASE loopzero TO loopzero_readonly;
    GRANT USAGE ON SCHEMA public TO loopzero_readonly;
    GRANT SELECT ON ALL TABLES IN SCHEMA public TO loopzero_readonly;
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO loopzero_readonly;
  END IF;
END $$;


-- ============================================================
-- 5. ROW-LEVEL SECURITY ON ALL TENANT-SCOPED TABLES (C2)
--
-- Policy design:
--   USING (tenant_id = current_setting('app.tenant_id', true)
--          OR current_setting('app.tenant_id', true) IS NULL)
--
-- When app.tenant_id IS SET: only rows matching that tenant are visible.
-- When app.tenant_id IS NOT SET (NULL): all rows visible (system queries).
--
-- This allows:
-- - User-facing API queries: SET LOCAL app.tenant_id → enforced isolation
-- - Reconciliation queries (FindActiveBuilds, FindPendingReleases): no SET → all rows
-- - Reapers (FindStaleBuilds, FindStaleReleases): no SET → all rows
-- ============================================================

-- builds
ALTER TABLE builds ENABLE ROW LEVEL SECURITY;
ALTER TABLE builds FORCE ROW LEVEL SECURITY;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'builds_tenant_isolation') THEN
    CREATE POLICY builds_tenant_isolation ON builds
      USING (tenant_id = current_setting('app.tenant_id', true)
             OR current_setting('app.tenant_id', true) IS NULL);
  END IF;
END $$;

-- releases
ALTER TABLE releases ENABLE ROW LEVEL SECURITY;
ALTER TABLE releases FORCE ROW LEVEL SECURITY;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'releases_tenant_isolation') THEN
    CREATE POLICY releases_tenant_isolation ON releases
      USING (tenant_id = current_setting('app.tenant_id', true)
             OR current_setting('app.tenant_id', true) IS NULL);
  END IF;
END $$;

-- environment_targets
ALTER TABLE environment_targets ENABLE ROW LEVEL SECURITY;
ALTER TABLE environment_targets FORCE ROW LEVEL SECURITY;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'environment_targets_tenant_isolation') THEN
    CREATE POLICY environment_targets_tenant_isolation ON environment_targets
      USING (tenant_id = current_setting('app.tenant_id', true)
             OR current_setting('app.tenant_id', true) IS NULL);
  END IF;
END $$;

-- secrets
ALTER TABLE secrets ENABLE ROW LEVEL SECURITY;
ALTER TABLE secrets FORCE ROW LEVEL SECURITY;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'secrets_tenant_isolation') THEN
    CREATE POLICY secrets_tenant_isolation ON secrets
      USING (tenant_id = current_setting('app.tenant_id', true)
             OR current_setting('app.tenant_id', true) IS NULL);
  END IF;
END $$;

-- projects
ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE projects FORCE ROW LEVEL SECURITY;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'projects_tenant_isolation') THEN
    CREATE POLICY projects_tenant_isolation ON projects
      USING (tenant_id = current_setting('app.tenant_id', true)
             OR current_setting('app.tenant_id', true) IS NULL);
  END IF;
END $$;

-- tenant_members
ALTER TABLE tenant_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_members FORCE ROW LEVEL SECURITY;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'tenant_members_tenant_isolation') THEN
    CREATE POLICY tenant_members_tenant_isolation ON tenant_members
      USING (tenant_id = current_setting('app.tenant_id', true)
             OR current_setting('app.tenant_id', true) IS NULL);
  END IF;
END $$;

-- audit_log
ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_log FORCE ROW LEVEL SECURITY;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'audit_log_tenant_isolation') THEN
    CREATE POLICY audit_log_tenant_isolation ON audit_log
      USING (tenant_id = current_setting('app.tenant_id', true)
             OR current_setting('app.tenant_id', true) IS NULL);
  END IF;
END $$;

-- idempotency_keys
ALTER TABLE idempotency_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE idempotency_keys FORCE ROW LEVEL SECURITY;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'idempotency_keys_tenant_isolation') THEN
    CREATE POLICY idempotency_keys_tenant_isolation ON idempotency_keys
      USING (tenant_id = current_setting('app.tenant_id', true)
             OR current_setting('app.tenant_id', true) IS NULL);
  END IF;
END $$;

-- sse_events: has build_id FK but no tenant_id — skip RLS
-- build_apps: has build_id FK but no tenant_id — skip RLS
-- circuit_breakers: system table, no tenant_id — skip RLS
-- users: no tenant_id — skip RLS
-- tenants: tenants table itself, no tenant_id column — skip RLS


-- ============================================================
-- 6. DEFERRED AUDIT TRIGGERS (A.14)
-- All triggers are CONSTRAINT TRIGGER DEFERRABLE INITIALLY DEFERRED,
-- meaning they fire at COMMIT time, OUTSIDE advisory lock transactions.
-- This has zero impact on lock hold time.
-- ============================================================

-- 6a. Append-only enforcement on audit_log
CREATE OR REPLACE FUNCTION audit_log_prevent_mutation()
RETURNS TRIGGER AS $$
BEGIN
  RAISE EXCEPTION 'audit_log is append-only: % not allowed', TG_OP;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- Drop existing trigger if present (idempotent re-run)
DROP TRIGGER IF EXISTS trg_audit_log_immutable ON audit_log;

CREATE CONSTRAINT TRIGGER trg_audit_log_immutable
  AFTER UPDATE OR DELETE ON audit_log
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW
  EXECUTE FUNCTION audit_log_prevent_mutation();


-- 6b. Audit trigger for sensitive table mutations (secrets, tenant_members)
CREATE OR REPLACE FUNCTION audit_sensitive_mutation()
RETURNS TRIGGER AS $$
DECLARE
  v_tenant_id TEXT;
  v_resource_id TEXT;
BEGIN
  -- Extract tenant_id from NEW (INSERT/UPDATE) or OLD (DELETE)
  IF TG_OP = 'DELETE' THEN
    v_tenant_id := OLD.tenant_id;
  ELSE
    v_tenant_id := NEW.tenant_id;
  END IF;

  -- Extract resource identifier
  IF TG_OP = 'DELETE' THEN
    IF TG_TABLE_NAME = 'secrets' THEN
      v_resource_id := OLD.id;
    ELSE
      v_resource_id := OLD.tenant_id || ':' || OLD.user_id;
    END IF;
  ELSE
    IF TG_TABLE_NAME = 'secrets' THEN
      v_resource_id := NEW.id;
    ELSE
      v_resource_id := NEW.tenant_id || ':' || NEW.user_id;
    END IF;
  END IF;

  INSERT INTO audit_log (tenant_id, action, actor, resource_type, resource_id, correlation_id, metadata)
  VALUES (
    v_tenant_id,
    TG_OP,
    COALESCE(current_setting('app.current_user', true), 'system'),
    TG_TABLE_NAME,
    v_resource_id,
    current_setting('app.correlation_id', true),
    jsonb_build_object('trigger', true, 'table', TG_TABLE_NAME, 'op', TG_OP)
  );

  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Secrets audit trigger
DROP TRIGGER IF EXISTS trg_secrets_audit ON secrets;
CREATE CONSTRAINT TRIGGER trg_secrets_audit
  AFTER INSERT OR UPDATE OR DELETE ON secrets
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW
  EXECUTE FUNCTION audit_sensitive_mutation();

-- Tenant members audit trigger
DROP TRIGGER IF EXISTS trg_tenant_members_audit ON tenant_members;
CREATE CONSTRAINT TRIGGER trg_tenant_members_audit
  AFTER INSERT OR UPDATE OR DELETE ON tenant_members
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW
  EXECUTE FUNCTION audit_sensitive_mutation();
