-- T2.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure-plan.md.
-- Per ADR D1: SLSA L3 SBOM-to-image attestation linking via cosign attest.
--
-- Adds nullable column releases.image_attestation_rekor_url to persist
-- the Rekor entry URL produced by the `cosign attest --predicate <sbom>`
-- step (T2.1). Nullable to preserve backward compat with existing
-- releases produced before attest step shipped (legacy = no attestation
-- chain; that's expected, not a bug).
--
-- Forward-only migration per pre-release governance (rollback impossible
-- without data loss; nullable column = no migration rollback impact).
--
-- Set on activation: build_reconcile_complete.go reads
-- workflow_output_parameters.rekor-url from the cosign-attest step's
-- output capture + persists here in same transaction as ImageSignature
-- + ImageDigestSHA256 (atomic enrichment of release row).

ALTER TABLE releases
  ADD COLUMN image_attestation_rekor_url TEXT NULL;

COMMENT ON COLUMN releases.image_attestation_rekor_url IS
  'Rekor entry URL produced by cosign attest --predicate <sbom> step (SLSA L3 chain). Empty/NULL when CosignAttestEnabled=false or attest step never ran. Per ADR-2026-05-27-T2.2-OFFLINE-TUF + plan T2.1 of 2026-05-27-tenant-build-pipeline-gaps-closure.';
