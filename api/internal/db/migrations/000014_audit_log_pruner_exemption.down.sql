-- 000014_audit_log_pruner_exemption.down.sql
-- Reverts the pruner bypass exemption — restores categorically-append-only behavior.

CREATE OR REPLACE FUNCTION audit_log_prevent_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audit_log is append-only: % not allowed', TG_OP;
  RETURN NULL;
END;
$$;
