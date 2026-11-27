-- ============================================================================
-- 000005_release_app_attestations
--
-- Sprint 6 / T0.1 — Per-app attestation sidecar table.
--
-- ADR-0014 §Multi-app builds: the existing `releases` table only persists
-- the signature for `Apps[0]` (image_digest, image_signature, image_signed_at).
-- This breaks the contract required by the admission policy "ALL images
-- signed". This sidecar table extends per-app supply-chain attestation
-- (signature + SBOM metadata) without backfilling the legacy `releases`
-- columns.
--
-- The columns sbom_s3_key / sbom_format / sbom_component_count are
-- populated by the syft scan step (Phase 1 of the sprint plan) and are
-- nullable because legacy releases (pre-Sprint-6) have no SBOM.
--
-- Forward-only migration (no DOWN file) per LoopRuntime conventions.
-- ============================================================================

CREATE TABLE IF NOT EXISTS release_app_attestations (
    id              TEXT PRIMARY KEY,
    release_id      TEXT NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    tenant_id       TEXT NOT NULL,
    app_name        TEXT NOT NULL,
    image_digest    TEXT NOT NULL,
    image_signature TEXT,
    signed_at       TIMESTAMPTZ,
    sbom_s3_key     TEXT,
    sbom_format     TEXT,
    sbom_component_count INTEGER,
    cell_id         TEXT NOT NULL DEFAULT 'cell-default',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT release_app_attestations_id_format CHECK (id LIKE 'raa-%'),
    CONSTRAINT release_app_attestations_digest_format
        CHECK (image_digest LIKE 'sha256:%'),
    CONSTRAINT release_app_attestations_sbom_format_value
        CHECK (sbom_format IS NULL
               OR sbom_format IN ('cyclonedx-json', 'spdx-json')),
    CONSTRAINT release_app_attestations_sbom_count_nonneg
        CHECK (sbom_component_count IS NULL OR sbom_component_count >= 0),

    -- One row per (release, app).
    CONSTRAINT release_app_attestations_unique_app
        UNIQUE (release_id, app_name)
);

-- Common query paths.
CREATE INDEX IF NOT EXISTS idx_release_app_attestations_release
    ON release_app_attestations (release_id);

CREATE INDEX IF NOT EXISTS idx_release_app_attestations_tenant
    ON release_app_attestations (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_release_app_attestations_signed
    ON release_app_attestations (tenant_id, signed_at)
    WHERE signed_at IS NOT NULL;

-- Row-level security follows the pattern used by builds/releases:
-- when app.tenant_id is set, restrict to that tenant; when unset, allow
-- (admin contexts and migrations bypass RLS via SET LOCAL or unset GUC).
ALTER TABLE release_app_attestations ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS release_app_attestations_tenant_isolation
    ON release_app_attestations;
CREATE POLICY release_app_attestations_tenant_isolation
    ON release_app_attestations
    USING (
        tenant_id = current_setting('app.tenant_id', true)
        OR current_setting('app.tenant_id', true) IS NULL
        OR current_setting('app.tenant_id', true) = ''
    );

-- Auto-fill cell_id from tenant on insert (matches builds/releases pattern).
DROP TRIGGER IF EXISTS trg_release_app_attestations_populate_cell_id
    ON release_app_attestations;
CREATE TRIGGER trg_release_app_attestations_populate_cell_id
    BEFORE INSERT ON release_app_attestations
    FOR EACH ROW EXECUTE FUNCTION populate_cell_id_from_tenant();

-- Maintain updated_at on every UPDATE.
CREATE OR REPLACE FUNCTION release_app_attestations_set_updated_at()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_release_app_attestations_updated_at
    ON release_app_attestations;
CREATE TRIGGER trg_release_app_attestations_updated_at
    BEFORE UPDATE ON release_app_attestations
    FOR EACH ROW EXECUTE FUNCTION release_app_attestations_set_updated_at();

COMMENT ON TABLE release_app_attestations IS
    'Per-app supply-chain attestation (signature + SBOM metadata) sidecar '
    'for releases (Sprint 6, ADR-0014 fix for multi-app builds).';

COMMENT ON COLUMN release_app_attestations.image_digest IS
    'OCI image digest (sha256:...). Same digest as in the Application CR.';

COMMENT ON COLUMN release_app_attestations.image_signature IS
    'Pinned reference (registry/path:tag@sha256:...) used by cosign sign. '
    'NULL when the build pre-dates Sprint 6 cosign signing or signing failed.';

COMMENT ON COLUMN release_app_attestations.sbom_s3_key IS
    'S3 object key pointing to the CycloneDX/SPDX SBOM (Sprint 6 syft step). '
    'NULL when SBOM generation is disabled or pre-dates Sprint 6.';
