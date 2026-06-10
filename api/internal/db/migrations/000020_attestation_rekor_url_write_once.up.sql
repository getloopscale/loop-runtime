-- T2.1 of plan 2026-05-28-meet-001-engineering-closure-plan.md.
-- CR-4 of MEET-2026-05-28-001: an attacker with PG mutation access can
-- overwrite a release row's Rekor URL to point at an attacker-controlled
-- mirror. Migration 000019 made the column writable from INSERT only by
-- convention; this migration makes it writable from INSERT only by
-- enforcement (BEFORE UPDATE trigger raising EXCEPTION) and adds an
-- AFTER INSERT trigger that appends a row to audit_log every time a
-- non-NULL Rekor URL is persisted.
--
-- Schema invariant after this migration:
--   releases.image_attestation_rekor_url
--     - NULL allowed at INSERT (legacy releases + keyless disabled)
--     - non-NULL allowed at INSERT (cosign attest success)
--     - UPDATE setting NULL → distinct → DISTINCT → reject (write-once)
--     - UPDATE re-setting same value → IS DISTINCT FROM is FALSE → allowed
--       (idempotent retries / no-op self-writes do NOT trigger the gate)
--
-- Forward-only per CLAUDE.md pre-release governance. Emergency rollback
-- (drop triggers) requires sponsor sign-off and is documented in the
-- T4.5 runbook (Sigstore-rollback). pgcrypto is already required by
-- migrations 000001/000009; this migration introduces no new extension.

-- ---------------------------------------------------------------------------
-- 1) BEFORE UPDATE: write-once enforcement.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION reject_attestation_url_mutation() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.image_attestation_rekor_url IS NOT NULL
     AND NEW.image_attestation_rekor_url IS DISTINCT FROM OLD.image_attestation_rekor_url THEN
    RAISE EXCEPTION
      'image_attestation_rekor_url is write-once (release_id=%, old=%, new=%)',
      OLD.id, OLD.image_attestation_rekor_url, NEW.image_attestation_rekor_url
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS releases_attestation_url_immutable ON releases;
CREATE TRIGGER releases_attestation_url_immutable
  BEFORE UPDATE ON releases
  FOR EACH ROW
  EXECUTE FUNCTION reject_attestation_url_mutation();

COMMENT ON FUNCTION reject_attestation_url_mutation IS
  'T2.1 / CR-4 (MEET-2026-05-28-001): prevent mutation of '
  'releases.image_attestation_rekor_url once it is set. SLSA L3 chain '
  'integrity gate — silent rewrite would let an attacker point future '
  'verification at a controlled Rekor mirror.';

-- ---------------------------------------------------------------------------
-- 2) AFTER INSERT: audit_log row when a non-NULL Rekor URL ships.
-- ---------------------------------------------------------------------------
--
-- audit_log schema (migration 000001:244-254): id SERIAL PK,
-- tenant_id, action, actor, resource_type, resource_id, correlation_id
-- (nullable), metadata (jsonb), created_at (default now()). We do NOT
-- pass id (SERIAL auto) or created_at (default).
--
-- correlation_id propagates from the release row when present; falls
-- back to NULL when the release was created without one (legacy /
-- system-initiated paths).
CREATE OR REPLACE FUNCTION audit_attestation_url_set() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.image_attestation_rekor_url IS NOT NULL THEN
    INSERT INTO audit_log
      (tenant_id, action, actor, resource_type, resource_id, correlation_id, metadata)
    VALUES
      (NEW.tenant_id,
       'release.attestation_url_set',
       'system:release-attestation',
       'release',
       NEW.id,
       NEW.correlation_id,
       jsonb_build_object(
         'rekor_url',  NEW.image_attestation_rekor_url,
         'build_id',   NEW.build_id,
         'project_id', NEW.project_id,
         'environment', NEW.environment));
  END IF;
  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS releases_attestation_url_audit ON releases;
CREATE TRIGGER releases_attestation_url_audit
  AFTER INSERT ON releases
  FOR EACH ROW
  EXECUTE FUNCTION audit_attestation_url_set();

COMMENT ON FUNCTION audit_attestation_url_set IS
  'T2.1 / CR-4 (MEET-2026-05-28-001): emits audit_log row with action '
  'release.attestation_url_set whenever a release lands with a non-NULL '
  'image_attestation_rekor_url. Pairs with the write-once trigger above '
  'to give SecOps a tamper-evident trail of every Rekor URL persisted.';
