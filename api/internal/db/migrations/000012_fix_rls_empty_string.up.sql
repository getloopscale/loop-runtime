-- Migration 000011: Fix RLS policies for empty string after set_config local reset.
--
-- When set_config('app.tenant_id', value, true) is used inside a transaction,
-- after COMMIT the value resets to '' (empty string), NOT NULL.
-- The original RLS policies only checked IS NULL, causing queries on pooled
-- connections to be blocked after any WithEnvironmentLock usage.
--
-- Fix: add OR current_setting('app.tenant_id', true) = '' to all policies.
-- Also: make audit_log.correlation_id nullable (seed/admin operations don't have one).

-- Fix RLS policies
DO $$
DECLARE
  tbl text;
BEGIN
  FOR tbl IN SELECT unnest(ARRAY['builds','releases','projects','secrets','environment_targets','tenant_members','audit_log','idempotency_keys'])
  LOOP
    EXECUTE format(
      'DROP POLICY IF EXISTS %I ON %I',
      tbl || '_tenant_isolation', tbl
    );
    EXECUTE format(
      'CREATE POLICY %I ON %I FOR ALL USING (
        tenant_id = current_setting(''app.tenant_id'', true)
        OR current_setting(''app.tenant_id'', true) IS NULL
        OR current_setting(''app.tenant_id'', true) = ''''
      )',
      tbl || '_tenant_isolation', tbl
    );
  END LOOP;
END $$;

-- Fix audit_log.correlation_id to be nullable (admin/seed operations don't have correlation_id)
ALTER TABLE audit_log ALTER COLUMN correlation_id DROP NOT NULL;

-- Fix CHECK constraints: migration 000009 missed states from the state machine.
-- builds: missing CANCELLING
ALTER TABLE builds DROP CONSTRAINT IF EXISTS chk_builds_status;
ALTER TABLE builds ADD CONSTRAINT chk_builds_status
  CHECK (status IN ('QUEUED', 'RUNNING', 'RELEASING', 'SUCCEEDED', 'FAILED', 'CANCELLING', 'CANCELLED'))
  NOT VALID;
ALTER TABLE builds VALIDATE CONSTRAINT chk_builds_status;

-- releases: missing GIT_MIRROR_FAILED, SYNC_FAILED, DRIFT_ALERT, ROLLING_BACK, ROLLBACK_FAILED
ALTER TABLE releases DROP CONSTRAINT IF EXISTS chk_releases_status;
ALTER TABLE releases ADD CONSTRAINT chk_releases_status
  CHECK (status IN ('READY', 'PENDING', 'ACTIVE', 'GIT_MIRROR_FAILED', 'SYNC_FAILED', 'DRIFT_ALERT', 'SUPERSEDED', 'ROLLING_BACK', 'ROLLED_BACK', 'ROLLBACK_FAILED', 'CANCELLED'))
  NOT VALID;
ALTER TABLE releases VALIDATE CONSTRAINT chk_releases_status;

-- Disable FORCE ROW LEVEL SECURITY on all tables.
-- The owner (loopzero) bypasses RLS by default — FORCE was incorrectly enabled.
-- Background services (reconcile loop, reaper, convergence loop) run as owner
-- without request context, so they cannot set app.tenant_id. With FORCE enabled,
-- these services get 0 rows from every query, breaking the entire build pipeline.
-- RLS still enforces for non-owner roles (loopzero_api, loopzero_readonly) when they're used.
ALTER TABLE builds NO FORCE ROW LEVEL SECURITY;
ALTER TABLE releases NO FORCE ROW LEVEL SECURITY;
ALTER TABLE projects NO FORCE ROW LEVEL SECURITY;
ALTER TABLE secrets NO FORCE ROW LEVEL SECURITY;
ALTER TABLE environment_targets NO FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_members NO FORCE ROW LEVEL SECURITY;
ALTER TABLE audit_log NO FORCE ROW LEVEL SECURITY;
ALTER TABLE idempotency_keys NO FORCE ROW LEVEL SECURITY;
