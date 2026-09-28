-- queries/teams.sql — sqlc query definitions for the tenant_members
-- + users tables (team membership domain).
--
-- T2.1 (Batch A #5) of plan 2026-05-29-sqlc-migration-plan
-- v1.3. tenant_members IS multi-tenant (in TenantOwnedTables); users
-- queries use id/username principal lookup paths (tenantpredicate:
-- exempt — JWT middleware authenticates upstream).
--
-- AUDIT-7: ListMembers + GetMember reads users.username (provider-
-- agnostic) via JOIN, replacing the earlier "github-" prefix strip
-- on user id.

-- name: ListTenantMembers :many
-- Returns all members of a tenant with their roles, ordered by role
-- then username for consistent UI output.
SELECT u.username, COALESCE(u.email, '') AS email, tm.role, tm.created_at
FROM tenant_members tm
JOIN users u ON tm.user_id = u.id
WHERE tm.tenant_id = $1
ORDER BY tm.role, u.username;

-- name: CountTenantMembers :one
-- Plan enforcement: seats in use, gated against plan_limits.seats.
SELECT COUNT(*)::int FROM tenant_members
WHERE tenant_id = $1;

-- name: AddTenantMember :exec
-- Idempotent: ON CONFLICT (tenant_id, user_id) DO NOTHING preserves
-- the original role + created_at when the same user is re-added.
INSERT INTO tenant_members (tenant_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (tenant_id, user_id) DO NOTHING;

-- name: RemoveTenantMember :exec
-- Idempotent: deleting a non-member returns nil (legacy contract).
DELETE FROM tenant_members
WHERE tenant_id = $1 AND user_id = $2;

-- name: UpdateTenantMemberRole :execrows
-- Wrapper maps rows-affected=0 → domain.ErrMemberNotFound so the
-- handler can surface 404 for non-members.
UPDATE tenant_members
SET role = $1
WHERE tenant_id = $2 AND user_id = $3;

-- name: GetTenantMember :one
-- Single-member lookup for authorization checks. AUDIT-7: surfaces
-- username via JOIN to users (provider-agnostic).
SELECT u.username, COALESCE(u.email, '') AS email, tm.role, tm.created_at
FROM tenant_members tm
JOIN users u ON tm.user_id = u.id
WHERE tm.tenant_id = $1 AND tm.user_id = $2;

-- tenantpredicate:exempt
-- name: GetUsernameByUserID :one
-- AUDIT-10: used by /whoami to surface a meaningful display when the
-- JWT has no email claim. Principal lookup (WHERE id = $1), so
-- cross-tenant by design — JWT middleware authenticates upstream.
SELECT username FROM users WHERE id = $1;

-- tenantpredicate:exempt
-- name: GetUserByUsername :one
-- AUDIT-7: provider-agnostic user lookup by username (no "github-"
-- prefix assumption). Principal lookup; JWT middleware authenticates
-- upstream.
SELECT id, COALESCE(email, '') AS email, COALESCE(display_name, '') AS display_name
FROM users WHERE username = $1;
