-- queries/membership.sql — sqlc query for user → tenant_members
-- lookup (middleware.MembershipStore contract).
--
-- T2.1 (Batch A #8a) of plan docs/plans/2026-05-29-sqlc-migration-plan.md
-- v1.3. The query operates by user_id (the principal) — cross-tenant
-- by design. JWT middleware authenticates upstream; the result set
-- enumerates the tenants the user is a member of (this is the
-- core "what tenants can I access?" lookup).

-- tenantpredicate:exempt
-- name: GetTenantMembershipsByUser :many
-- Returns all tenant memberships for a given user_id. The caller
-- is the user (JWT-authenticated); the lookup enumerates accessible
-- tenants. Empty result → empty slice via wrapper make() pattern.
SELECT tenant_id, role
FROM tenant_members
WHERE user_id = $1;
