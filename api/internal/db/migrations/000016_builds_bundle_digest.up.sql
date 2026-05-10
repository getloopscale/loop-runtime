-- 000016_builds_bundle_digest.up.sql
--
-- T2.2 of deep-review-2026-05-10-remediation-plan + ADR D4
-- (closes find_sec_no_bundle_integrity HIGH).
--
-- BACKGROUND
-- ----------
-- The CLI uploads a source bundle to S3 (MinIO/DO Spaces) before the
-- Argo Workflow consumes it. Pre-T2.2 there was no integrity check: a
-- malicious actor with S3 write access could substitute the bundle
-- between upload and workflow consumption, and `cosign sign` (which
-- runs AFTER BuildKit) would happily sign whatever bytes BuildKit
-- consumed — covering the substitution rather than detecting it.
--
-- This migration adds `bundle_digest TEXT` to the `builds` table. The
-- upload handler computes `sha256.Sum256` over the bundle bytes and
-- stores the hex string here. The Argo Workflow's first step
-- (`clone-runner`) verifies the digest before any user code is
-- evaluated. Mismatch → workflow fails → audit log captures the
-- discrepancy.
--
-- Nullable for backward compatibility: builds created before T2.2
-- have no digest. The workflow checks "if digest is set, verify it".
-- Once all in-flight builds drain, a follow-up migration may flip the
-- column to NOT NULL.

ALTER TABLE builds
    ADD COLUMN IF NOT EXISTS bundle_digest TEXT;

COMMENT ON COLUMN builds.bundle_digest IS
  'deep-review-2026-05-10 T2.2 — SHA256 hex of the uploaded source bundle, computed at upload time + verified by the clone-runner step. Mismatch indicates S3 substitution.';
