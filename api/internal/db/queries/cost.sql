-- queries/cost.sql — sqlc query definitions for the cost_counters
-- table. T2.2 (Batch B #5) of plan
-- 2026-05-29-sqlc-migration-plan v1.3.
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

-- ============================================================================
-- Compute metering producer (plan metering-compute-loop T2.3).
-- ADR-4 (same-period) + ADR-9 (period-split) atomic data-modifying CTEs.
-- period fns concretized as pure-UTC date_trunc (EC-7). Every SC guard is in
-- the CTE (SQL, not Go), governed by the single DB now() that advances the cursor.
-- ============================================================================

-- name: MeterCompute :one
-- ADR-4 same-period writer. Advances the cursor (CAS + SC-1 monotonicity + SC-2
-- same-period predicate + SC-6 fence) and, only if it advanced, adds the delta
-- to the cost_counters row (upsert). Returns the delta written (bigint) so the
-- wrapper can thread the SAME quantity into usage_records in the same tx. When
-- the cursor is HELD (lost CAS / backward clock / period straddle / stale fence),
-- `adv` yields 0 rows → the top-level SELECT returns 0 rows → pgx.ErrNoRows,
-- which the wrapper reads as "held, nothing metered" (skip usage). The data-
-- modifying CTEs execute regardless of the final SELECT (Postgres WITH semantics).
WITH adv AS (
  UPDATE metering_cursor SET last_metered_at = now()
  WHERE tenant_id = @tenant_id::text
    AND metric = @metric::text
    AND cell_id = @cell_id::text
    AND last_metered_at = @expected::timestamptz
    AND now() > metering_cursor.last_metered_at
    AND metering_period_of(now()) = metering_period_of(@expected::timestamptz)
    AND @gen::bigint = (SELECT generation_id FROM leader_generations
                        WHERE name = @loop_name::text AND cell_id = @leader_cell::text)
  RETURNING GREATEST(LEAST(now() - @expected::timestamptz, @max_delta::interval), interval '0') AS elapsed
),
ins AS (
  INSERT INTO cost_counters (tenant_id, metric, period_start, period_end, plan_tier, value)
    SELECT @tenant_id::text, @metric::text,
           metering_period_start(@expected::timestamptz),
           metering_period_end(@expected::timestamptz),
           @plan_tier::text,
           metering_cost(@req_millicore::bigint, elapsed)
    FROM adv
    ON CONFLICT (tenant_id, metric, period_start)
      DO UPDATE SET value = cost_counters.value + EXCLUDED.value, plan_tier = EXCLUDED.plan_tier
)
SELECT metering_cost(@req_millicore::bigint, elapsed) AS delta,
       metering_period_start(@expected::timestamptz) AS period_start,
       metering_period_end(@expected::timestamptz) AS period_end
FROM adv;

-- name: MeterComputeSplit :one
-- ADR-9 period-rollover writer. Fires ONLY on a real straddle (SC-2 <> predicate).
-- Advances the cursor to now() (SC-4 continuation, never reset to the boundary),
-- then splits the max_delta-CLAMPED window from expected to LEAST(now, expected plus
-- max_delta) at the boundary: tail to P1 (expected's period), head to P2 (the next).
-- SC-7 (MEET-006): clamping the TOTAL bounds over-billing symmetrically with
-- MeterCompute AND guarantees at most one boundary (no middle-period misattribution).
-- Returns the CLAMPED TOTAL delta (tail_compute + head_compute) so the wrapper threads
-- the same bounded quantity into usage_records. HELD (no straddle / lost CAS / stale
-- fence) → `adv` yields 0 rows → 0-row SELECT → pgx.ErrNoRows (wrapper: skip usage).
WITH adv AS (
  UPDATE metering_cursor SET last_metered_at = now()
  WHERE tenant_id = @tenant_id::text
    AND metric = @metric::text
    AND cell_id = @cell_id::text
    AND last_metered_at = @expected::timestamptz
    AND now() > metering_cursor.last_metered_at
    AND metering_period_of(now()) <> metering_period_of(@expected::timestamptz)
    AND @gen::bigint = (SELECT generation_id FROM leader_generations
                        WHERE name = @loop_name::text AND cell_id = @leader_cell::text)
  -- SC-7 (MEET-006): clamp the TOTAL gap to max_delta, then split the clamped
  -- window at the boundary (metering_split_tail/head, migration 000048). The clamp
  -- keeps the window under one period past expected (ADR-8): at most one boundary,
  -- P2 is exactly the next period, tail plus head equals the clamped total.
  RETURNING
    metering_split_tail(@expected::timestamptz, @max_delta::interval) AS tail,
    metering_split_head(@expected::timestamptz, @max_delta::interval) AS head
),
p1 AS (
  INSERT INTO cost_counters (tenant_id, metric, period_start, period_end, plan_tier, value)
    SELECT @tenant_id::text, @metric::text,
           metering_period_start(@expected::timestamptz),
           metering_period_end(@expected::timestamptz),
           @plan_tier::text,
           metering_cost(@req_millicore::bigint, tail)
    FROM adv
    ON CONFLICT (tenant_id, metric, period_start)
      DO UPDATE SET value = cost_counters.value + EXCLUDED.value, plan_tier = EXCLUDED.plan_tier
),
p2 AS (
  -- P2 is the period immediately after expected's period (metering_period_end of
  -- expected is that period's start), NOT the period of now(): a long clamped gap
  -- must not key head into now()'s period. The clamp guarantees head lands here.
  INSERT INTO cost_counters (tenant_id, metric, period_start, period_end, plan_tier, value)
    SELECT @tenant_id::text, @metric::text,
           metering_period_end(@expected::timestamptz),
           metering_period_end(metering_period_end(@expected::timestamptz)),
           @plan_tier::text,
           metering_cost(@req_millicore::bigint, head)
    FROM adv
    ON CONFLICT (tenant_id, metric, period_start)
      DO UPDATE SET value = cost_counters.value + EXCLUDED.value, plan_tier = EXCLUDED.plan_tier
)
SELECT metering_cost(@req_millicore::bigint, tail) AS tail_cost,
       metering_cost(@req_millicore::bigint, head) AS head_cost,
       metering_period_start(@expected::timestamptz) AS p1_start,
       metering_period_end(@expected::timestamptz) AS p1_end,
       metering_period_end(@expected::timestamptz) AS p2_start,
       metering_period_end(metering_period_end(@expected::timestamptz)) AS p2_end
FROM adv;

-- name: GetMeteringEnabled :one
-- Read the observe-only kill-switch (metering_config singleton, migration 000046).
-- The loop reads this per-tick and skips the pass when disabled. EC-5: an absent
-- row → pgx.ErrNoRows → the wrapper defaults to DISABLED (fail-safe). No RLS.
SELECT enabled FROM metering_config WHERE id = true;

-- name: GetMeteringCursor :one
-- Read the current cursor value — the CAS target the worker threads into
-- MeterInput.Expected each tick (ADR-4/ADR-9: last_metered_at = @expected). Pure
-- read; the wrapper absorbs pgx.ErrNoRows to (zero, found=false) so the worker
-- seeds first (SeedMeteringCursor, SC-5) then reads. metering_cursor has no RLS.
SELECT last_metered_at FROM metering_cursor
WHERE tenant_id = @tenant_id::text
  AND metric = @metric::text
  AND cell_id = @cell_id::text;

-- name: SeedMeteringCursor :exec
-- Seed-if-absent to now() on first sight (EC-10 — zero charge for the pre-existence
-- window). Idempotent: an existing cursor is untouched.
INSERT INTO metering_cursor (tenant_id, metric, cell_id, last_metered_at)
VALUES (@tenant_id::text, @metric::text, @cell_id::text, now())
ON CONFLICT (tenant_id, metric, cell_id) DO NOTHING;

-- name: UpsertUsageRecord :exec
-- Same-tx accumulate into the Stripe-reported usage feed (BIGINT quantity, mig 000047).
-- ONE row per (tenant, metric, period_start) — enforced by the UNIQUE index
-- idx_usage_tenant_metric_period (000001). The conflict target MUST be that tuple,
-- NOT the id PK: a fresh @id per tick would never hit an id-conflict, so the 2nd
-- write of a period would violate the unique index (hard error) instead of
-- accumulating. @id is a fresh UUID used only when the period's row is first created.
INSERT INTO usage_records (id, tenant_id, metric, quantity, period_start, period_end)
VALUES (@id::text, @tenant_id::text, @metric::text, @quantity::bigint, @period_start::timestamptz, @period_end::timestamptz)
ON CONFLICT (tenant_id, metric, period_start) DO UPDATE SET quantity = usage_records.quantity + EXCLUDED.quantity;

-- name: GetPlanTierForTenant :one
-- EC-4: plan_tier from the subscriptions BILLING status (NOT tenant lifecycle).
-- Missing/none → caller defaults to 'starter' + loopruntime_metering_missing_plan_total WARN.
SELECT COALESCE(plan, 'starter')::text AS plan_tier
FROM subscriptions
WHERE tenant_id = @tenant_id::text AND status = 'active';

-- name: GetCellForTenant :one
-- The canonical placement SoT (INV I2 / tenant-guardian C1). Run in the
-- SET LOCAL app.tenant_id='' RLS-bypass context. NoRows → caller skips (harmless).
SELECT cell_id FROM tenants WHERE id = @tenant_id::text;
