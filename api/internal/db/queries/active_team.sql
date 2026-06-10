-- queries/active_team.sql — sqlc query definitions for explicit
-- user-driven active-team selection (migration 000018, users.
-- active_tenant_id column). Plan T2.5 D8.
--
-- T2.1 of plan docs/plans/2026-05-29-sqlc-migration-plan.md v1.3
-- (Phase 2 Batch A — 2nd of 10 stores).
--
-- All queries here are USER-scoped (WHERE id = $1 on the users
-- table) — the caller has already authenticated via JWT and is
-- mutating their OWN active-team selection. Cross-tenant by design
-- (a user is a principal that can be a member of multiple tenants),
-- so all queries are tenantpredicate:exempt. The users table is in
-- TenantOwnedTables for OTHER access paths (owner_id / active_tenant_id
-- filters) but the active-team store operates at the user-principal
-- level.

-- tenantpredicate:exempt
-- name: GetActiveTenantForUser :one
-- Returns the user's explicit active_tenant_id, or NULL if the user
-- never set an explicit selection (legacy default — fall back to JWT
-- middleware heuristic). Wrapper maps NULL → empty string.
SELECT active_tenant_id FROM users WHERE id = $1;

-- tenantpredicate:exempt
-- name: SetActiveTenantForUser :exec
-- Persists the user's explicit active-team selection. FK enforces
-- tenant exists; ON DELETE SET NULL handles deletion. Caller MUST
-- validate the user is a member of the tenant (handler-level
-- enforcement per plan T2.5 D8).
UPDATE users
SET active_tenant_id = $1, updated_at = NOW()
WHERE id = $2;

-- tenantpredicate:exempt
-- name: ClearActiveTenantForUser :exec
-- Nulls the user's active_tenant_id. Used by EC-4 self-heal in /me
-- handler when the persisted reference points to a tenant the user
-- is no longer a member of (tenant deleted by admin or membership
-- revoked).
UPDATE users
SET active_tenant_id = NULL, updated_at = NOW()
WHERE id = $1;
