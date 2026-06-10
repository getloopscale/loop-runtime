-- queries/build_gc.sql — sqlc query definitions for the garbage-
-- collection portions of the builds + github_webhook_deliveries
-- tables. T2.2 (Batch B #6) of plan
-- docs/plans/2026-05-29-sqlc-migration-plan.md v1.3.
--
-- NOTE: DeleteOldBuilds (the window-function ROW_NUMBER() OVER
-- (PARTITION BY ...) ORDER BY ... subquery) is NOT in this file.
-- sqlc 1.31.1 cannot parse the ranked-subquery shape (`column rn
-- does not exist` errors across multiple CTE/subquery refactors).
-- Per ADR-1 D5 of docs/decisions/2026-05-29-sqlc-adoption.md, that
-- query lives in build_gc_wrapper.go as raw SQL via DBTX.Exec —
-- pending T4.1 raw/ subpackage adoption for sqlc-unparseable
-- queries.

-- name: DeleteOldWebhookDeliveries :execrows
-- Removes GitHub webhook delivery dedup records older than cutoff.
-- Admin GC scheduler path (no tenant_id — github_webhook_deliveries
-- is admin-scoped: it tracks the (delivery_id, event_type,
-- received_at) tuple per GitHub PRIMARY KEY delivery_id, used to
-- dedupe replay events at the API webhook handler).
DELETE FROM github_webhook_deliveries
WHERE received_at < $1;
