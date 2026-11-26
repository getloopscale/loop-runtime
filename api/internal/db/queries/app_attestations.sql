-- queries/app_attestations.sql — sqlc query definitions for the
-- release_app_attestations table.
--
-- T1.3 of plan 2026-05-29-sqlc-migration-plan v1.3 (Phase
-- 1 pilot — M07 SLSA L3 attestation flow + F-S2-01 FK violation race
-- closure). This store is the canonical per-app sidecar of
-- ADR-0014 (multi-app attestation fix) and ships the tx-batched
-- persistence path that closes the legacy FK race where an attestation
-- INSERT could land before the release row commit.
--
-- Per ADR-1 D2 (pgx/v5) + D3 (emit_methods_with_db_argument) + D6
-- (schema=schema.gen.sql) + EC-3 (snake_case JSON tags via sqlc.yaml).
--
-- Multi-tenant SELECT / UPDATE / DELETE carries `tenant_id` predicate
-- per platform-security-engineer condition 1 + M-1 + M-2 (enforced by
-- cmd/loop-runtime-ops/internal/quality/tenantpredicate gate). Cross-tenant queries marked
-- `-- tenantpredicate:exempt` IMMEDIATELY ABOVE `-- name:`.
--
-- The UPSERT shape preserves the "ALL-signed contract" — every app row
-- is validated by the wrapper BEFORE any INSERT (partial writes would
-- leak signatures for some apps but not others, violating the SLSA L3
-- guarantee that an attestation set is complete-or-absent).

-- name: UpsertAppAttestation :one
-- Idempotent on (release_id, app_name) UNIQUE constraint. The
-- ON CONFLICT clause uses COALESCE(EXCLUDED, current) so a partial
-- update (signature-only, sbom-only) preserves whichever columns the
-- caller did not supply. The wrapper invokes this once per
-- AppAttestation row, after pre-validating digest format + non-empty
-- app_name across the whole slice.
INSERT INTO release_app_attestations (
  id,
  release_id,
  tenant_id,
  app_name,
  image_digest,
  image_signature,
  signed_at,
  sbom_s3_key,
  sbom_format,
  sbom_component_count
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
ON CONFLICT (release_id, app_name) DO UPDATE SET
  image_digest = EXCLUDED.image_digest,
  image_signature = COALESCE(EXCLUDED.image_signature, release_app_attestations.image_signature),
  signed_at = COALESCE(EXCLUDED.signed_at, release_app_attestations.signed_at),
  sbom_s3_key = COALESCE(EXCLUDED.sbom_s3_key, release_app_attestations.sbom_s3_key),
  sbom_format = COALESCE(EXCLUDED.sbom_format, release_app_attestations.sbom_format),
  sbom_component_count = COALESCE(EXCLUDED.sbom_component_count, release_app_attestations.sbom_component_count)
RETURNING *;

-- name: GetBuildAppAttestationsForTenant :many
-- Returns all per-app attestations for the MOST RECENT release of the
-- given build, scoped to the tenant. Cross-tenant queries return empty
-- (no existence leak per ADR-0014). The correlated subquery picks the
-- MAX(created_at) release row for the build+tenant tuple — there can
-- be multiple PROGRESSING/FAILED releases historically; we only surface
-- the latest.
SELECT raa.app_name,
       raa.image_digest,
       raa.image_signature,
       raa.signed_at,
       raa.sbom_s3_key,
       raa.sbom_format,
       raa.sbom_component_count,
       raa.created_at
FROM release_app_attestations raa
INNER JOIN releases r ON r.id = raa.release_id
WHERE r.build_id = $1
  AND r.tenant_id = $2
  AND r.created_at = (
    SELECT MAX(created_at) FROM releases
    WHERE build_id = $1 AND tenant_id = $2
  )
ORDER BY raa.app_name ASC;

-- tenantpredicate:exempt
-- name: UpdateAppSBOM :execrows
-- Updates SBOM metadata for one (release, app) row. Called by
-- build_reconcile_complete after the syft workflow step extracts SBOM
-- output; caller has authenticated the release ownership upstream via
-- the build reconcile loop's release fetch. Returns rows-affected so
-- the wrapper can surface "no matching row" as a typed error (legacy
-- contract: fmt.Errorf "no row matched").
UPDATE release_app_attestations
SET sbom_s3_key = $1,
    sbom_format = $2,
    sbom_component_count = $3
WHERE release_id = $4 AND app_name = $5;

-- name: ListSignedAppImagesForCommit :many
-- Returns, per app, the newest signed image any earlier build of the given
-- tenant, project and commit pushed. The image tag is the commit SHA and
-- tenant repositories have immutable tags, so a new build of that commit
-- reuses these images instead of pushing a rebuild the registry refuses.
-- Only rows with a signature and a signing time qualify: an unsigned image is
-- never reused.
SELECT DISTINCT ON (raa.app_name)
       raa.app_name,
       raa.image_digest,
       raa.image_signature,
       raa.signed_at,
       raa.sbom_s3_key,
       raa.sbom_format,
       raa.sbom_component_count
FROM release_app_attestations raa
INNER JOIN releases r ON r.id = raa.release_id AND r.tenant_id = raa.tenant_id
INNER JOIN builds b ON b.id = r.build_id AND b.tenant_id = r.tenant_id
WHERE raa.tenant_id = $1
  AND b.project_id = $2
  AND b.commit_sha = $3
  AND raa.image_signature IS NOT NULL
  AND raa.signed_at IS NOT NULL
ORDER BY raa.app_name, raa.created_at DESC;
