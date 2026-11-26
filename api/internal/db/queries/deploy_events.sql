-- queries/deploy_events.sql — sqlc query definitions for the deploy_events
-- transactional-outbox table (migration 000051).
--
-- J-16 Settings slice 3 foundation (MEET-2026-07-03-005, Option C). The ENGINE
-- emits one row per terminal deploy transition, INSIDE the release-activation /
-- build-failed tx (atomic event capture). loop-console drains + fans out; the
-- engine never reads this table (the drain is a cloud-side, cross-tenant system
-- query, not an engine sqlc query), so no ListDeployEvents lives here.
--
-- Per ADR-1 D2 (pgx/v5) + D3 (emit_methods_with_db_argument): the generated
-- InsertDeployEvent takes a DBTX first arg so callers pass the activation tx.
--
-- deploy_events is NOT in tenantpredicate.TenantOwnedTables — consistent with
-- memory_webhooks / workspace_webhooks (webhook-family tables); the engine's
-- only query here is a tenant-scoped INSERT (no cross-tenant read-leak surface).

-- name: InsertDeployEvent :exec
-- Append a deploy terminal-state event to the outbox. Called by the release
-- activation kernel on the SAME tx as the state transition (transactional
-- outbox). transition_id is UNIQUE — a duplicate INSERT for the same transition
-- errors, which is the intended guard.
INSERT INTO deploy_events (
  transition_id, event_type, tenant_id, project_id, deploy_id, correlation_id, payload
) VALUES (
  $1, $2, $3, $4, $5, $6, $7
);
