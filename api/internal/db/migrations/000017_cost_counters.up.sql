-- Migration 000017 — cost_counters table for per-tenant cap enforcement.
--
-- Plan: T1.9 of plans/2026-05-12-launch-readiness-waves-plan.md (v1.4).
-- Slot correction per MEET-ASYNC-AMENDMENT-2026-05-12-001 (was 000012,
-- occupied by 000012_fk_on_delete_explicit).
--
-- Distinction vs `usage_records`:
--   - usage_records  = append-only history reported to Stripe (billing)
--   - cost_counters  = mutable current-state counter for HTTP 402 enforcement
--
-- Both tables can be incremented by the aggregator from the same metric
-- source; their downstream consumers differ (Stripe reporter vs middleware
-- enforcement) and so do their retention semantics (history retained for
-- audit/tax; counters reset per period boundary).
--
-- See MEET-ASYNC-AMENDMENT-2026-05-12-001 § A003 for the design decision
-- ratifying separate table over extending usage_records.

CREATE TABLE IF NOT EXISTS cost_counters (
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    -- plan_tier snapshot at counter write time. Redundant with subscriptions.plan
    -- but kept inline so enforcement reads do not need to JOIN. Updated by the
    -- aggregator each tick — if the tenant downgrades mid-period, the next
    -- aggregator pass overwrites with the current plan.
    plan_tier TEXT NOT NULL CHECK (plan_tier IN ('starter', 'pro', 'team', 'enterprise')),
    metric TEXT NOT NULL CHECK (metric IN ('egress_bytes', 'docr_pulls', 'build_minutes')),
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    -- BIGINT because egress can reach 10 TB = 10^13 bytes on Team plan.
    value BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Composite PK: one counter per (tenant, metric, period).
    -- The aggregator UPSERTs on this triple; enforcement reads by the same.
    PRIMARY KEY (tenant_id, metric, period_start)
);

-- Enforcement read path: SELECT value WHERE tenant_id=? AND metric=? AND now() BETWEEN period_start AND period_end.
-- Composite PK already covers (tenant_id, metric, period_start).
-- An extra partial index on the *current* period would be nice but Postgres
-- cannot use `now()` in a partial index predicate (volatile function), so we
-- rely on the PK + B-tree lookup which is O(log n) on (tenant_id, metric).

-- RLS policy mirroring the pattern from migration_000001 for tenant tables.
-- Enforcement middleware reads with `SET app.tenant_id = caller_tenant`; the
-- aggregator runs as cloud control plane (no tenant context set → bypass).
ALTER TABLE cost_counters ENABLE ROW LEVEL SECURITY;
CREATE POLICY cost_counters_tenant_isolation ON cost_counters
  USING (tenant_id = current_setting('app.tenant_id', true)
         OR current_setting('app.tenant_id', true) IS NULL
         OR current_setting('app.tenant_id', true) = '');

-- Backfill comment for documentation only — no rows are inserted here.
-- The aggregator will populate counters on its first tick after this
-- migration ships. Enforcement gracefully degrades to "allow" when no row
-- exists yet (counter=0 implies under cap for all plans).
COMMENT ON TABLE cost_counters IS
  'Per-tenant cost cap enforcement state. T1.9 of MEET-2026-05-12 launch readiness. '
  'Separate from usage_records (billing history) per MEET-ASYNC-AMENDMENT-2026-05-12-001 A003.';
