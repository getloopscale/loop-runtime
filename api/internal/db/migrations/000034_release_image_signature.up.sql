-- Sprint 6: Supply chain security — track cosign signature per release.
-- image_digest: the sha256 digest of the container image (from BuildKit metadata).
-- image_signature: cosign signature reference (tag in the registry, e.g., sha256-abc123.sig).
-- image_signed_at: timestamp when the image was signed (NULL if not yet signed).
-- These fields enable the GET /api/v1/builds/:id/attestation endpoint
-- and support admission policy enforcement (verify before deploy).

ALTER TABLE releases
    ADD COLUMN image_digest text,
    ADD COLUMN image_signature text,
    ADD COLUMN image_signed_at timestamp with time zone;

COMMENT ON COLUMN releases.image_digest IS 'sha256 digest of the container image from BuildKit';
COMMENT ON COLUMN releases.image_signature IS 'cosign signature reference in the registry';
COMMENT ON COLUMN releases.image_signed_at IS 'Timestamp when cosign signed the image';
