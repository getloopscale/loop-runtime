-- Migration 000029: Denormalize cell_id onto builds and releases
-- Roadmap 03 — Cell-Scoped Queries (MEET-2026-04-10-046)
-- ADR-0008: Cell-Based Architecture
--
-- Why denormalize instead of JOIN?
--   1. At scale (20 cells × 5k tenants) FindPendingReleases runs every few
--      seconds. A JOIN to tenants on every reconcile tick adds unnecessary
--      query planning and buffer traffic.
--   2. PG table partitioning by cell_id (tracked as escalation risk in
--      roadmap/index.md) REQUIRES cell_id as a column on the partitioned
--      table. Denormalization is a prerequisite for future partition pruning.
--   3. Backward compatibility: existing rows and any INSERT that does not
--      specify cell_id fall back to 'cell-default', matching current behavior.
--
-- Forward-only: no DOWN migration required (additive column + indexes).

-- =============================================================================
-- 1. Add cell_id column to builds
-- =============================================================================
ALTER TABLE builds
    ADD COLUMN IF NOT EXISTS cell_id TEXT NOT NULL DEFAULT 'cell-default';

-- Backfill from tenants.cell_id for rows created before this migration.
-- The DEFAULT already handles new rows, but existing rows must be migrated
-- so future cell-scoped queries return them in the correct cell.
UPDATE builds b
SET    cell_id = t.cell_id
FROM   tenants t
WHERE  b.tenant_id = t.id
  AND  b.cell_id = 'cell-default'
  AND  t.cell_id IS DISTINCT FROM 'cell-default';

-- Composite index: the most common cell-scoped query is
-- `WHERE status IN (...) AND cell_id = $N ORDER BY created_at`.
-- (cell_id, status, created_at) covers that access pattern.
CREATE INDEX IF NOT EXISTS idx_builds_cell_status_created
    ON builds (cell_id, status, created_at);

COMMENT ON COLUMN builds.cell_id IS
    'Denormalized from tenants.cell_id for cell-scoped reconciliation queries. See ADR-0008 and roadmap 03.';

-- =============================================================================
-- 2. Add cell_id column to releases
-- =============================================================================
ALTER TABLE releases
    ADD COLUMN IF NOT EXISTS cell_id TEXT NOT NULL DEFAULT 'cell-default';

UPDATE releases r
SET    cell_id = t.cell_id
FROM   tenants t
WHERE  r.tenant_id = t.id
  AND  r.cell_id = 'cell-default'
  AND  t.cell_id IS DISTINCT FROM 'cell-default';

CREATE INDEX IF NOT EXISTS idx_releases_cell_status_created
    ON releases (cell_id, status, created_at);

COMMENT ON COLUMN releases.cell_id IS
    'Denormalized from tenants.cell_id for cell-scoped reconciliation queries. See ADR-0008 and roadmap 03.';

-- =============================================================================
-- 3. Trigger: on INSERT, copy cell_id from tenants if caller did not set it
-- =============================================================================
-- Rationale: the Go code INSERT statements do not (yet) pass cell_id
-- explicitly. This trigger ensures correctness without requiring a
-- simultaneous rewrite of every CreateBuild/CreateRelease call site.
-- When callers DO pass a non-default cell_id, the trigger leaves it alone.

CREATE OR REPLACE FUNCTION populate_cell_id_from_tenant()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.cell_id = 'cell-default' OR NEW.cell_id IS NULL THEN
        SELECT cell_id INTO NEW.cell_id
        FROM   tenants
        WHERE  id = NEW.tenant_id;
        IF NEW.cell_id IS NULL THEN
            NEW.cell_id := 'cell-default';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_builds_populate_cell_id ON builds;
CREATE TRIGGER trg_builds_populate_cell_id
    BEFORE INSERT ON builds
    FOR EACH ROW
    EXECUTE FUNCTION populate_cell_id_from_tenant();

DROP TRIGGER IF EXISTS trg_releases_populate_cell_id ON releases;
CREATE TRIGGER trg_releases_populate_cell_id
    BEFORE INSERT ON releases
    FOR EACH ROW
    EXECUTE FUNCTION populate_cell_id_from_tenant();
