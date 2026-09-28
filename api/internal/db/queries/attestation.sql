-- queries/attestation.sql — sqlc queries for the
-- supply-chain attestation summary endpoint (domain.AttestationStore
-- contract via PostgresAttestationStore).
--
-- T2.1 (Batch A #8c) of plan 2026-05-29-sqlc-migration-plan
-- v1.3. The store reads the LATEST release for a build and surfaces
-- its supply-chain columns (image_digest, image_signature,
-- image_signed_at) as AttestationInfo. CHG-2026-04-25-ATTESTATION-WIRE
-- Phase 6 invariants:
--   - Cross-tenant reads → ErrBuildNotFound (avoid existence leak,
--     ADR-0014 trust model)
--   - Empty signature fields → signed=false (legacy releases pre-
--     Phase 5)

-- name: GetLatestReleaseAttestationByBuildForTenant :one
-- Tenant-isolated variant: cross-tenant reads return NoRows (mapped
-- to ErrBuildNotFound in wrapper, NOT ErrForbidden) to avoid
-- existence leaks per ADR-0014 trust model.
SELECT image_digest, image_signature, image_signed_at
FROM releases
WHERE build_id = $1 AND tenant_id = $2
ORDER BY created_at DESC
LIMIT 1;
