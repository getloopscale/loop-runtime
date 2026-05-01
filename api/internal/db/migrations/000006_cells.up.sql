-- Migration 000006: cells table — operational source of truth for cell metadata.
-- (Renumbered from 000003 — that slot was consumed before this migration was
-- authored, so golang-migrate would silently skip it on existing DBs at
-- schema_migrations.version >= 5. See CHANGELOG entry [Unreleased] for fix
-- discovered LIVE 2026-04-30 during multi-cell validation.)
--
-- Design context (per docs/audits/multi-cell-source-audit.md):
--   • ClusterCredential CRDs (K8s) are SoT for cell *connectivity*
--     (kubeconfig location, status.connected, scope to ArgoCD/operators).
--     Read by K8sCellStore.LoadCells() at api/internal/placement/k8s_cell_store.go
--     and by DynamicRouter on 60s refresh.
--   • This `cells` table is SoT for cell *operational metadata*: capacity gates,
--     audit trail of cell registration, deterministic ordering for placement
--     decisions, and persistence of cell state independent of K8s API
--     availability. Without it, capacity policy ("don't pin tenant to full
--     cell") and audit ("when was cell-2 activated, by whom?") have no home.
--   • The two SoTs are not redundant — they answer different questions.
--     A reconciler keeps them consistent (planned T5.4 observability +
--     cell-status reconciler in operators).
--
-- Forward-only migration. INSERTs are idempotent (ON CONFLICT DO NOTHING).
-- Seeded with cell-default + cell-1 + cell-2 so existing tenants
-- (cell_id='cell-default' DEFAULT) resolve cleanly when the API queries
-- this table for capacity checks.
--
-- Cross-references:
--   • api/internal/db/migrations/000001_initial_schema.up.sql — defines
--     tenants.cell_id with DEFAULT 'cell-default' (line 89) + populate_cell_
--     id_from_tenant() trigger (line 59).
--   • operators/api/v1/clustercredential_types.go — CC.spec.cellID pattern
--     `^cell-[a-z0-9-]+$`. This table's CHECK constraint matches.
--   • api/internal/placement/dynamic_router.go — Resolve(cellID) is the
--     consumer that benefits when this table tracks capacity (future).
--   • docs/decisions/MEET-INFRA-NORMALIZE-T3-multi-cell.md — references
--     this migration as part of cell-2 activation gates.

CREATE TABLE IF NOT EXISTS cells (
    -- Primary key. Matches CC.spec.cellID pattern.
    cell_id              TEXT        PRIMARY KEY
                                     CHECK (cell_id ~ '^cell-[a-z0-9-]+$'),

    -- Operator-friendly label distinct from cell_id (e.g., "Cell 2 (sfo3)").
    display_name         TEXT        NOT NULL,

    -- Operational status. Drives placement eligibility:
    --   active     : accept new tenant pinning + reconcile existing
    --   draining   : reject new tenant pinning, allow existing reconcile
    --   maintenance: reject all routing decisions (operator-imposed pause)
    --   retired    : terminal — no traffic; cells_id remains for FK history
    status               TEXT        NOT NULL DEFAULT 'active'
                                     CHECK (status IN ('active', 'draining',
                                                        'maintenance', 'retired')),

    -- Region hint for capacity planning + customer comms.
    -- Format: free-form, e.g., 'sfo3' (DigitalOcean) or 'us-east-1' (AWS).
    region               TEXT        NOT NULL DEFAULT 'unknown',

    -- Capacity gate for placement decisions. NULL = unlimited (e.g., default
    -- cell pre-multi-cell activation). 0 = blocked (effectively draining
    -- without status flip — useful for transient capacity preservation).
    -- A row gating with capacity_tenants_max IS NOT NULL must be enforced
    -- by the placement layer before INSERT into tenants.
    capacity_tenants_max INTEGER     CHECK (capacity_tenants_max IS NULL
                                            OR capacity_tenants_max >= 0),

    -- Operator notes — RCA references, customer commitment, commissioning
    -- doc URL. Free text.
    notes                TEXT        NOT NULL DEFAULT '',

    -- Audit trail. Forward-only (UPDATEs welcome on display_name, status,
    -- region, capacity_tenants_max, notes — but cell_id is immutable).
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Index for queries scanning by status (placement layer's most common
-- filter: "all active cells with capacity available").
CREATE INDEX IF NOT EXISTS idx_cells_status_capacity
    ON cells (status, capacity_tenants_max);

-- Trigger: auto-update updated_at on every UPDATE.
-- Reuses the trigger function pattern from 000001_initial_schema (search
-- for `set_updated_at` if it exists; if not, define inline-equivalent).
CREATE OR REPLACE FUNCTION set_cells_updated_at()
RETURNS trigger AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_cells_set_updated_at ON cells;
CREATE TRIGGER trg_cells_set_updated_at
    BEFORE UPDATE ON cells
    FOR EACH ROW EXECUTE FUNCTION set_cells_updated_at();

-- Seed three baseline cells.
--   cell-default : preserves the DEFAULT 'cell-default' value of
--                  tenants.cell_id from migration 000001. Status active,
--                  unlimited capacity. Existing tenants reference it
--                  implicitly via DEFAULT.
--   cell-1       : the canonical first cell. Pre-multi-cell activation,
--                  this is logically the same cluster as cell-default
--                  in single-cell deployments. The cells.cell_id column
--                  represents the *named* cell; the DEFAULT exists for
--                  backwards compatibility.
--   cell-2       : the runtime-2 cluster (per CLAUDE.md "Cell architecture
--                  Phase 0+1+1.5 complete; runtime-2 physically provisioned
--                  but not routed"). Seeded as 'maintenance' so placement
--                  rejects pinning until the activation canary
--                  (plan v2 Phase 5 stage 1+2) flips it to 'active'.
--                  This makes cell-2 visible to operators (capacity gate
--                  exists, audit trail begins) without admitting traffic.
INSERT INTO cells (cell_id, display_name, status, region, notes)
VALUES
    ('cell-default', 'Default Cell',
                     'active', 'unknown',
                     'Backwards-compat default for tenants.cell_id DEFAULT. ' ||
                     'Pre-multi-cell, all tenants referenced this implicitly.'),
    ('cell-1',       'Cell 1 (runtime)',
                     'active', 'sfo3',
                     'Canonical first cell. Maps to runtime-1 DOKS cluster ' ||
                     'in production deployments.'),
    ('cell-2',       'Cell 2 (runtime-2)',
                     'maintenance', 'sfo3',
                     'Physically provisioned (infra.ts:160-175) but not ' ||
                     'admitting traffic. Activation gated by plan v2 Phase 5 ' ||
                     'canary stages 1-2 (synthetic + 10% dev) and Phase 7 ' ||
                     'production canary stages 4-5 (1 customer + tiered).')
ON CONFLICT (cell_id) DO NOTHING;

COMMENT ON TABLE cells IS 'Operational source of truth for cell metadata (capacity, status, audit). Connectivity SoT remains ClusterCredential CRD. Two SoTs answer different questions.';

COMMENT ON COLUMN cells.status IS
    'active=accept new pinning; draining=existing only; maintenance=reject all; retired=terminal.';

COMMENT ON COLUMN cells.capacity_tenants_max IS
    'NULL=unlimited; 0=blocked transiently; positive=hard cap enforced by placement layer.';
