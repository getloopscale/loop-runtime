-- Migration 000011: Add cell_id to tenants table
-- ADR-0008: Cell-Based Architecture for 50k+ app scale
-- Phase 0: Foundation — adds cell_id field with default "cell-0"
-- Forward-only: no DROP statements

-- cell_id identifies which Cell a tenant's workloads run in.
-- Phase 0: all tenants get "cell-0" (single Cell = current behavior).
-- Phase 1+: PlacementEngine assigns new tenants to specific Cells.
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS cell_id TEXT NOT NULL DEFAULT 'cell-0';

-- Index for Phase 1+ queries: "find all tenants in Cell X"
CREATE INDEX IF NOT EXISTS idx_tenants_cell_id ON tenants(cell_id);

-- Audit comment
COMMENT ON COLUMN tenants.cell_id IS 'Cell assignment for tenant workloads. See ADR-0008.';
