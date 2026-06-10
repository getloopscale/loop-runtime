-- queries/cost.sql — sqlc query definitions for the cost_counters
-- table. T2.2 (Batch B #5) of plan
-- docs/plans/2026-05-29-sqlc-migration-plan.md v1.3.
--
-- T1.9 production wiring (Wave 1 of MEET-2026-05-12):
-- enforcement.CostStore reads + cost_counters cap policy.
-- Composite PK on (tenant_id, metric, period_start) makes this
-- query an O(log n) lookup.
--
-- Fail-open semantics (matching the legacy CostStore interface):
--   - Row missing (pgx.ErrNoRows) → wrapper returns (zero, false, nil)
--     CapChecker interprets this as "no usage yet this period" = allow
--   - DB error → wrapper returns (zero, false, err) — CapChecker
--     treats this as fail-open (degraded aggregator must never block
--     traffic)

-- name: GetActiveCostCounter :one
-- Returns the active counter for (tenant, metric) where now() is
-- within the period window. Epoch seconds (bigint) returned for
-- direct mapping to enforcement.CostCounter int64 fields.
SELECT plan_tier,
       EXTRACT(EPOCH FROM period_start)::bigint AS period_start_epoch,
       EXTRACT(EPOCH FROM period_end)::bigint AS period_end_epoch,
       value
FROM cost_counters
WHERE tenant_id = $1
  AND metric = $2
  AND now() >= period_start
  AND now() <  period_end;
