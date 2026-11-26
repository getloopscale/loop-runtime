-- 000014_audit_log_pruner_exemption.up.sql
--
-- T7.3 of deep-review-remediation-plan + ADR-0047 (audit_log retention).
--
-- BACKGROUND
-- ----------
-- The audit_log immutability trigger created in migration 000001
-- (trg_audit_log_immutable + audit_log_prevent_mutation) raises
-- EXCEPTION on every DELETE/UPDATE. This makes the table
-- categorically append-only — including blocking the legitimate
-- retention pruner from removing rows older than the retention window.
--
-- Per ADR-0047 the retention policy is 1 year for security/operational
-- events. Implementing the pruner requires a controlled exception:
-- the trigger function consults session config 'loopruntime.audit_pruner'
-- and skips the EXCEPTION raise when set to 'true'. This is the
-- audit-trail equivalent of "you must explicitly request bypass" —
-- accidental DELETE from psql, application code, or a runaway query
-- still raises EXCEPTION because they will not have set the var.
--
-- The pruner service in api/internal/services/audit_log_pruner.go
-- sets it via `SET LOCAL` inside a transaction so the bypass scope
-- is limited to the pruner's own SQL.

CREATE OR REPLACE FUNCTION audit_log_prevent_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
  v_pruner_bypass TEXT;
BEGIN
  -- T7.3: allow controlled DELETE by the audit_log_pruner service
  -- when it sets `SET LOCAL loopruntime.audit_pruner = 'true'` inside a
  -- transaction. Any other caller (psql, app code, runaway query)
  -- has NULL or empty here and falls through to the EXCEPTION.
  v_pruner_bypass := current_setting('loopruntime.audit_pruner', true);
  IF TG_OP = 'DELETE' AND v_pruner_bypass = 'true' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'audit_log is append-only: % not allowed', TG_OP;
  RETURN NULL;
END;
$$;

COMMENT ON FUNCTION audit_log_prevent_mutation() IS
  'deep-review-remediation T7.3 — allows pruner DELETE when session var loopruntime.audit_pruner=true; all other mutations rejected.';
