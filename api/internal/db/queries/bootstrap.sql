-- queries/bootstrap.sql — sqlc query definitions for the platform
-- bootstrap flow. T2.2 (Batch B #3) of plan
-- docs/plans/2026-05-29-sqlc-migration-plan.md v1.3.
--
-- All 4 queries are idempotent INSERTs (ON CONFLICT DO NOTHING) so
-- repeated bootstrap calls are safe. The wrapper tracks "did anyone
-- actually insert?" via :execrows return + accumulator pattern
-- preserving the legacy `anyCreated` semantics.
--
-- MEET-053 D-01: the wrapper accepts a DBTX which may be the
-- TxFromContext-injected SERIALIZABLE transaction during AssignCell
-- placement (fixes TOCTOU race where two concurrent creates could
-- both pick the same cell). Caller picks the DBTX (pool vs tx);
-- sqlc-generated queries dispatch unchanged.

-- tenantpredicate:exempt — users PK by id (provider-prefixed). The
-- users table is tenant-scoped via owner_id/active_tenant_id on
-- write paths, not on this bootstrap INSERT which establishes the
-- identity record.
-- name: InsertBootstrapUser :execrows
-- AUDIT-7: username is the provider-agnostic identity column. For
-- GitHub bootstrap, it equals the admin login. ON CONFLICT (id) DO
-- NOTHING preserves the existing row on re-bootstrap.
INSERT INTO users (id, username, email, github_id, display_name)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO NOTHING;

-- name: InsertBootstrapTenant :execrows
-- ADR-0008: cell_id assigned by PlacementEngine via
-- BootstrapRequest.CellID (defaults to 'cell-default' for Phase 0
-- when wrapper-side default fires). ON CONFLICT (id) DO NOTHING.
INSERT INTO tenants (id, display_name, slug, owner_id, cell_id)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO NOTHING;

-- name: InsertBootstrapTenantMember :execrows
-- ON CONFLICT (tenant_id, user_id) DO NOTHING — composite key.
INSERT INTO tenant_members (tenant_id, user_id, role)
VALUES ($1, $2, 'owner')
ON CONFLICT (tenant_id, user_id) DO NOTHING;

-- name: InsertBootstrapSubscription :execrows
-- §1.1: subscriptions is single source of truth for plan. Plan
-- comes from BootstrapRequest.Plan (wrapper-side default 'starter'
-- when empty). ON CONFLICT (tenant_id) DO NOTHING — single-row-per-
-- tenant invariant.
INSERT INTO subscriptions (id, tenant_id, plan, status, created_at, updated_at)
VALUES ($1, $2, $3, 'active', now(), now())
ON CONFLICT (tenant_id) DO NOTHING;
