-- queries/audit.sql — sqlc query definitions for the audit_log table.
--
-- T1.5 of plan 2026-05-29-sqlc-migration-plan v1.3 (Phase
-- 1 pilot — sensitive store; M-3 audit-callsite-diff + M-4 pair-review
-- gate per MEET-2026-05-29-001).
--
-- Per ADR-1 D2 (pgx/v5) + D3 (emit_methods_with_db_argument) + D6
-- (schema=schema.gen.sql) + EC-3 (snake_case JSON tags via sqlc.yaml).
--
-- Multi-tenant — every query carries `tenant_id = $1` (enforced by
-- cmd/loopzero-ops/internal/quality/tenantpredicate gate). audit_log is in TenantOwnedTables.
--
-- Dynamic-filter pattern for ListAuditLog uses sqlc nullable arg form:
-- (cast::type IS NULL OR column = cast::type). When the filter slot is
-- NULL the predicate short-circuits to TRUE; when populated it applies.
-- Same plan-cache shape regardless of filter combination, vs the
-- legacy fmt.Sprintf-string-concat approach (which emitted different
-- SQL for each combination = N×M×K cache slots).

-- name: InsertAuditLog :exec
-- Application-level audit trail (MEET-2026-03-31-024 A.13). Database-
-- level triggers provide defense-in-depth (migration 000009).
-- correlation_id + metadata are nullable per schema.
INSERT INTO audit_log (
  tenant_id, action, actor, resource_type, resource_id, correlation_id, metadata
) VALUES (
  $1, $2, $3, $4, $5, $6, $7
);

-- name: ListAuditLogForTenant :many
-- Paginated audit list with optional filters. Uses sqlc.narg() form
-- for nullable args so the wrapper passes nil/zero when the filter
-- is empty — same SQL shape regardless of filter combination. The
-- COALESCE on correlation_id matches legacy behavior (NULL → '' so
-- domain.AuditEntryResult.CorrelationID plain-string scans cleanly,
-- preserving the finding #26 fix).
SELECT action,
       resource_type,
       resource_id,
       actor,
       COALESCE(correlation_id, '') AS correlation_id,
       metadata,
       created_at
FROM audit_log
WHERE tenant_id = @tenant_id::text
  AND (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action')::text)
  AND (sqlc.narg('resource_type')::text IS NULL OR resource_type = sqlc.narg('resource_type')::text)
  AND (sqlc.narg('since')::timestamptz IS NULL OR created_at >= sqlc.narg('since')::timestamptz)
ORDER BY created_at DESC
LIMIT @lim::int OFFSET @off::int;

-- name: CountAuditLogForTenant :one
-- Total row count matching the same (tenant, filters) predicate as
-- ListAuditLogForTenant — pagination depends on total being computed
-- against an identical WHERE shape.
SELECT COUNT(*) FROM audit_log
WHERE tenant_id = @tenant_id::text
  AND (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action')::text)
  AND (sqlc.narg('resource_type')::text IS NULL OR resource_type = sqlc.narg('resource_type')::text)
  AND (sqlc.narg('since')::timestamptz IS NULL OR created_at >= sqlc.narg('since')::timestamptz);
