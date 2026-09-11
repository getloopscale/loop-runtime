-- queries/idempotency.sql — sqlc query definitions for the
-- idempotency_keys table.
--
-- T2.1 (Batch A) of plan 2026-05-29-sqlc-migration-plan
-- v1.3 — Phase 2 store conversion. Multi-tenant: every query carries
-- `tenant_id` predicate (idempotency_keys is in TenantOwnedTables).
-- TTL is 24 hours per spec §4.3; the wrapper computes expires_at at
-- insert time.

-- name: GetIdempotencyRecord :one
-- Retrieves an active (non-expired) idempotency record scoped by
-- (key, tenant_id). NULL row → pgx.ErrNoRows; wrapper maps to nil.
SELECT key, tenant_id, endpoint, request_hash, response_status, response_body
FROM idempotency_keys
WHERE key = $1
  AND tenant_id = $2
  AND expires_at > NOW();

-- name: InsertIdempotencyRecord :exec
-- Stores an idempotency record. expires_at is supplied by the caller
-- (wrapper computes NOW + 24h per spec §4.3) so the SQL stays pure.
INSERT INTO idempotency_keys (
  key, tenant_id, endpoint, request_hash, response_status, response_body, expires_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7
);

-- tenantpredicate:exempt
-- name: CleanupExpiredIdempotencyKeys :execrows
-- Background maintenance — removes ALL expired keys regardless of
-- tenant. Cross-tenant by design (called from a background sweeper,
-- not user-flow). Returns the count for observability.
DELETE FROM idempotency_keys
WHERE expires_at < NOW();
