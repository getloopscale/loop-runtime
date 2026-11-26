-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Migration 000048 — pure-UTC billing-period functions (plan metering-compute-loop
-- T2.3; the ADR-4/ADR-9 `period_start()`/`period_end()`/`period_of()` made concrete).
--
-- Encapsulating the UTC month-boundary math as IMMUTABLE SQL functions (a) keeps
-- the metering CTEs sqlc-parseable (inline `date_trunc(... AT TIME ZONE 'UTC')`
-- with @named params confuses sqlc's param inference), (b) centralizes EC-7's
-- pure-UTC determinism in ONE place (never server-local / DST), (c) matches the
-- ADR's function naming. `metering_period_of` returns the period-start, so a
-- same-period test is `metering_period_of(a) = metering_period_of(b)`.

CREATE OR REPLACE FUNCTION metering_period_start(ts timestamptz) RETURNS timestamptz
  LANGUAGE sql IMMUTABLE PARALLEL SAFE AS
$$ SELECT date_trunc('month', ts AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' $$;

CREATE OR REPLACE FUNCTION metering_period_end(ts timestamptz) RETURNS timestamptz
  LANGUAGE sql IMMUTABLE PARALLEL SAFE AS
$$ SELECT metering_period_start(ts) + interval '1 month' $$;

-- period identity for the SC-2 same-period / split predicates (the UTC month start).
CREATE OR REPLACE FUNCTION metering_period_of(ts timestamptz) RETURNS timestamptz
  LANGUAGE sql IMMUTABLE PARALLEL SAFE AS
$$ SELECT metering_period_start(ts) $$;

-- Counter delta: reserved millicore × elapsed seconds / 1000 = cpu_core_seconds
-- (or mem-GB × seconds). Encapsulated so the CTE's `@req_millicore` param is not
-- glommed by sqlc into the adjacent EXTRACT(EPOCH …) expression.
CREATE OR REPLACE FUNCTION metering_cost(req_millicore bigint, dur interval) RETURNS bigint
  LANGUAGE sql IMMUTABLE PARALLEL SAFE AS
$$ SELECT (req_millicore * EXTRACT(EPOCH FROM dur) / 1000)::bigint $$;

-- SC-7 (MEET-2026-07-01-006): the clamped split of a boundary-straddling advance.
-- The billed window is the max_delta-CLAMPED [expected, LEAST(now(), expected+max_delta)];
-- `tail` is the part in expected's period (→ P1), `head` the part in the next period
-- (→ P2). Clamping the TOTAL (not just head) bounds over-billing symmetrically with
-- metering_cost's same-period clamp AND, since the clamped window stays under one
-- period past expected (ADR-8), guarantees at most one boundary — so head can never
-- be misattributed into a middle month. STABLE (not IMMUTABLE) because now() is read;
-- all now() reads in one statement/tx are identical, so tail+head is deterministic.
-- Encapsulated as functions because the inline nested LEAST/GREATEST confuses sqlc's
-- parser (same rationale as the period functions above).
CREATE OR REPLACE FUNCTION metering_split_tail(expected timestamptz, max_delta interval) RETURNS interval
  LANGUAGE sql STABLE PARALLEL SAFE AS
$$ SELECT GREATEST(LEAST(metering_period_end(expected), LEAST(now(), expected + max_delta)) - expected, interval '0') $$;

CREATE OR REPLACE FUNCTION metering_split_head(expected timestamptz, max_delta interval) RETURNS interval
  LANGUAGE sql STABLE PARALLEL SAFE AS
$$ SELECT GREATEST(LEAST(now(), expected + max_delta) - metering_period_end(expected), interval '0') $$;
