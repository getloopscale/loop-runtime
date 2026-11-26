-- queries/cells.sql — sqlc query definitions for the cell-aware
-- placement primitives. T2.2 (Batch B #2) of plan
-- 2026-05-29-sqlc-migration-plan v1.3.
--
-- Tables touched:
--   - tenants (tenant-owned self-table; uses `id = $N` not `tenant_id`)
--   - cells (operational metadata; NOT in TenantOwnedTables — admin
--     scope by design)
--
-- ADR-0008 + MEET-053: CountByCell + GetTenantCellID feed the
-- PlacementEngine (RoundRobinEngine.AssignAndExecute).
-- PinTenantToCell wires `loopruntime admin tenants pin` (CHG-2026-04-30-
-- DELETE-SINGLE-CELL) — atomic 4-step sequence:
--   1. validate tenant exists (lookup)
--   2. validate cell exists + is active
--   3. capacity check (NULL=unlimited; current excludes moving tenant)
--   4. UPDATE tenants.cell_id RETURNING previous cell_id
-- Steps 1-3 are SELECT-only (no exempt needed — `tenants.id = $N`
-- satisfies the tenant-self predicate; `cells` is not tenant-owned).
-- Step 4 mutates tenants by id — also satisfies the tenant-self
-- predicate.

-- name: CountTenantsByCellID :many
-- Returns (cell_id, count) for every cell that has at least one
-- tenant assigned. Used by CellMetricsCollector to emit
-- loopruntime_cell_tenant_count gauges + PlacementEngine for round-robin
-- bin-packing.
-- tenantpredicate:exempt — aggregated across all tenants by design;
-- routing/metrics paths are admin-scope.
SELECT cell_id, COUNT(*)::bigint AS tenant_count
FROM tenants
GROUP BY cell_id;

-- name: GetCellIDByTenantID :one
-- Returns the cell_id assigned to a specific tenant. Used by the API
-- routes (loopruntime deploy, loopruntime status, etc.) to dispatch through
-- DynamicRouter. pgx.ErrNoRows surfaces as a typed error in the
-- wrapper (not transparently nil, so the caller can distinguish
-- missing tenant from cross-cluster fault).
SELECT cell_id FROM tenants WHERE id = $1;

-- name: LookupTenantForCellPin :one
-- Pre-flight #1 of PinTenantToCell: existence check by tenant id.
-- Wrapper maps pgx.ErrNoRows → ErrCellPinnerTenantNotFound.
SELECT cell_id FROM tenants WHERE id = $1;

-- name: LookupCellForPin :one
-- Pre-flight #2 of PinTenantToCell: existence + status + capacity
-- in a single round-trip. Wrapper:
--   - pgx.ErrNoRows → ErrCellPinnerCellNotFound
--   - status != 'active' → ErrCellPinnerNotPinnable
--   - capacity is *int; NULL means unlimited
SELECT status, capacity_tenants_max FROM cells WHERE cell_id = $1;

-- name: CountTenantsInCellExcluding :one
-- Pre-flight #3 of PinTenantToCell: capacity check. Excludes the
-- moving tenant so re-pin-to-same-cell is OK.
SELECT COUNT(*)::bigint AS current_count
FROM tenants
WHERE cell_id = $1 AND id <> $2;

-- name: UpdateTenantCellID :exec
-- Step #4 of PinTenantToCell: atomic UPDATE. Wrapper returns the
-- previous cell_id captured from step #1.
UPDATE tenants SET cell_id = $1 WHERE id = $2;

-- name: ListDeclaredCellCapacities :many
-- Every cell that declares a tenant capacity. Feeds the PlacementEngine, so
-- automatic placement reads the same column PinTenantToCell enforces; for
-- automatic placement a cell with NULL falls back to the platform-wide
-- MAX_TENANTS_PER_CELL. `cells` is not tenant-owned.
SELECT cell_id, capacity_tenants_max::integer AS capacity_tenants_max
FROM cells
WHERE capacity_tenants_max IS NOT NULL;
