-- queries/bootstrap.sql — sqlc query definitions for the platform
-- bootstrap flow (POST /internal/bootstrap).
--
-- The inserts are idempotent (ON CONFLICT DO NOTHING) so repeated
-- bootstrap calls are safe. The wrapper tracks "did anyone actually
-- insert?" via :execrows, preserving the `created` semantics.
--
-- They run inside the placement transaction (MEET-053 D-01: the wrapper
-- picks the TxFromContext-injected transaction), which main opens on the
-- cross-tenant background pool: the tenant being created does not exist
-- yet, so no tenant scope can be set, and tenants/subscriptions/
-- tenant_members are under fail-closed RLS (migration 000062).

-- tenantpredicate:exempt — users PK by id (the identity, a Kratos UUID or
-- the legacy github-<login>). This INSERT establishes the identity record.
-- name: InsertBootstrapUser :execrows
-- AUDIT-7: username is the provider-agnostic identity column. ON CONFLICT
-- (id) DO NOTHING preserves the existing row on re-bootstrap.
INSERT INTO users (id, username, email, github_id, display_name)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO NOTHING;

-- tenantpredicate:exempt — accounts has no tenant_id; one Account per owner
-- (UNIQUE owner_id, M2). The Account owns the tenant and its subscription.
-- name: InsertBootstrapAccount :execrows
INSERT INTO accounts (id, display_name, plan, owner_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (owner_id) DO NOTHING;

-- tenantpredicate:exempt — the owner's Account, by its unique owner_id.
-- name: GetBootstrapAccountIDByOwner :one
SELECT id FROM accounts WHERE owner_id = $1;

-- tenantpredicate:exempt — the tenant row being created, keyed by its id.
-- name: InsertBootstrapTenant :execrows
-- ADR-0008: cell_id is the one the placement engine assigned. A conflict on
-- id OR on the unique slug inserts nothing; the wrapper then resolves the
-- slug's tenant and refuses it unless the caller owns it.
INSERT INTO tenants (id, display_name, slug, owner_id, cell_id, account_id)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT DO NOTHING;

-- tenantpredicate:exempt — tenants.slug is unique across tenants; this is
-- the lookup that decides whether the slug is the caller's.
-- name: GetBootstrapTenantBySlug :one
SELECT id, owner_id FROM tenants WHERE slug = $1;

-- name: InsertBootstrapTenantMember :execrows
-- ON CONFLICT (tenant_id, user_id) DO NOTHING — composite key.
INSERT INTO tenant_members (tenant_id, user_id, role)
VALUES ($1, $2, 'owner')
ON CONFLICT (tenant_id, user_id) DO NOTHING;

-- name: InsertBootstrapSubscription :execrows
-- §1.1: subscriptions is the single source of truth for plan. One
-- subscription per Account (UNIQUE account_id, M2).
INSERT INTO subscriptions (id, tenant_id, account_id, plan, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, 'active', now(), now())
ON CONFLICT (account_id) DO NOTHING;

-- tenantpredicate:exempt — users row of the identity just bootstrapped.
-- name: SetBootstrapActiveTenantIfUnset :exec
-- The first tenant an identity owns becomes its active tenant; an explicit
-- choice already made is never overwritten.
UPDATE users SET active_tenant_id = $2, updated_at = now()
WHERE id = $1 AND active_tenant_id IS NULL;
