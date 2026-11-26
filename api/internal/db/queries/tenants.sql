-- queries/tenants.sql — sqlc query definitions for the tenant lifecycle
-- domain.
--
-- T1.4 of plan 2026-05-29-sqlc-migration-plan v1.3 (Phase
-- 1 pilot — multi-tenant SoT + soft-delete lifecycle + admin bulk
-- eviction). Covers BOTH legacy stores: PostgresTenantStore (admin
-- bulk operations) AND PostgresTenantDeletionStore (soft-delete
-- lifecycle per MEET-052).
--
-- Per ADR-1 D2 (pgx/v5) + D3 (emit_methods_with_db_argument) + D6
-- (schema=schema.gen.sql) + EC-3 (snake_case JSON tags via sqlc.yaml).
--
-- All ADMIN queries (ListTenantIDsByPattern + 8-step FK-ordered
-- eviction) are tenantpredicate:exempt by design — the caller is
-- already authenticated as an operator and needs to read/write across
-- tenants. LOOPRUNTIME_ADMIN_API gate at the HTTP layer enforces the operator
-- authentication.
--
-- DELETION-LIFECYCLE queries (RequestDeletion, CancelDeletion,
-- GetPendingDeletion, WriteTombstone) are KEYED by tenant_id —
-- tenants_pending_deletion + tenant_tombstones tables are in the
-- TenantOwnedTables allowlist (extended in T1.4 of this plan).

-- =========================================================================
-- ADMIN BULK OPERATIONS (PostgresTenantStore)
-- =========================================================================

-- tenantpredicate:exempt
-- name: ListTenantIDsByPattern :many
-- Returns tenant IDs matching a SQL LIKE pattern, ordered ASC for
-- determinism. LIMITED to 10_000 rows to prevent pathological patterns
-- from materializing the entire tenants table. Caller validates the
-- pattern (rejects dangerous free-text like '%') BEFORE invoking. The
-- ESCAPE '\' clause allows literal _/% characters when needed.
--
-- The `::text` cast on the param forces sqlc to emit `string` (not
-- `[]byte`) — without it sqlc infers bytea from the LIKE+ESCAPE shape.
SELECT id FROM tenants
WHERE id LIKE $1::text ESCAPE '\'
ORDER BY id ASC
LIMIT 10000;

-- tenantpredicate:exempt
-- name: AdminEvictEnvironmentTargets :execrows
-- Step 1 of 8 — FK-safe bulk eviction. Removes environment_targets for
-- the supplied tenant IDs (legacy pattern: `= ANY($1::text[])`).
DELETE FROM environment_targets
WHERE tenant_id = ANY($1::text[]);

-- tenantpredicate:exempt
-- name: AdminEvictReleases :execrows
-- Step 2 of 8 — releases reference builds (NOT tenants directly); use
-- sub-SELECT against builds.
DELETE FROM releases
WHERE build_id IN (SELECT id FROM builds WHERE tenant_id = ANY($1::text[]));

-- tenantpredicate:exempt
-- name: AdminEvictBuildApps :execrows
-- Step 3 of 8 — build_apps reference builds; same sub-SELECT pattern.
DELETE FROM build_apps
WHERE build_id IN (SELECT id FROM builds WHERE tenant_id = ANY($1::text[]));

-- tenantpredicate:exempt
-- name: AdminEvictBuilds :execrows
-- Step 4 of 8 — builds directly tenant-owned.
DELETE FROM builds
WHERE tenant_id = ANY($1::text[]);

-- tenantpredicate:exempt
-- name: AdminEvictProjects :execrows
-- Step 5 of 8 — projects directly tenant-owned.
DELETE FROM projects
WHERE tenant_id = ANY($1::text[]);

-- tenantpredicate:exempt
-- name: AdminEvictSubscriptions :execrows
-- Step 6 of 8 — subscriptions directly tenant-owned (billing per-tenant).
DELETE FROM subscriptions
WHERE tenant_id = ANY($1::text[]);

-- tenantpredicate:exempt
-- name: AdminEvictTenantMembers :execrows
-- Step 7 of 8 — tenant_members directly tenant-owned.
DELETE FROM tenant_members
WHERE tenant_id = ANY($1::text[]);

-- tenantpredicate:exempt
-- name: AdminEvictTenants :execrows
-- Step 8 of 8 — final hard delete of the tenants row. Uses `id` (the
-- self-table identity column) per TenantSelfTable convention.
DELETE FROM tenants
WHERE id = ANY($1::text[]);

-- name: AdminEvictMeteringCursors :execrows
-- SC-5 (ADR-13, plan metering-compute-loop) — GC the compute-metering cursors on
-- tenant eviction, so a reused/reprovisioned tenant_id cannot bill a stale
-- [old_last_metered_at, now()] window (resurrection double-bill). metering_cursor
-- has no FK to tenants, so ordering vs the other evict steps does not matter.
DELETE FROM metering_cursor
WHERE tenant_id = ANY($1::text[]);

-- =========================================================================
-- SOFT-DELETE LIFECYCLE (PostgresTenantDeletionStore — MEET-052)
-- =========================================================================

-- name: InsertPendingDeletion :exec
-- Idempotent INSERT: ON CONFLICT DO NOTHING preserves the original
-- requested_at / requested_by when the same tenant is re-requested
-- (matches legacy behavior). Callers immediately GetPendingDeletion to
-- read back the canonical record.
INSERT INTO tenants_pending_deletion
  (tenant_id, requested_by, reason, grace_until, force)
VALUES
  ($1, $2, $3, $4, $5)
ON CONFLICT (tenant_id) DO NOTHING;

-- name: CancelPendingDeletion :exec
-- Idempotent DELETE: returns nil error when no pending deletion exists
-- for the tenant.
DELETE FROM tenants_pending_deletion
WHERE tenant_id = $1;

-- name: GetPendingDeletion :one
-- Returns the pending deletion record for a tenant, or pgx.ErrNoRows
-- when none exists (wrapper maps ErrNoRows → (nil, nil) per legacy
-- contract).
SELECT tenant_id, requested_at, requested_by, reason, grace_until, force, attempt_count
FROM tenants_pending_deletion
WHERE tenant_id = $1;

-- name: InsertTombstone :exec
-- Writes the permanent audit record after tenant deletion completes.
-- Idempotent on tenant_id (re-running with same input is a no-op).
INSERT INTO tenant_tombstones
  (tenant_id, tenant_slug, requested_by, reason, resources_cleaned, resources_failed)
VALUES
  ($1, $2, $3, $4, $5, $6)
ON CONFLICT (tenant_id) DO NOTHING;
