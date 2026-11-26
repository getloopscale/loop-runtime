-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Migration 000046 — Compute metering schema (plan metering-compute-loop T2.1).
--
-- (a) Widen cost_counters.metric CHECK to admit the two compute metrics so the
--     producer can write cpu_core_seconds / mem_gb_seconds counters.
-- (b) metering_cursor — per-(tenant, metric, cell) high-water mark of the last
--     metered instant. PK is (tenant_id, metric, cell_id) with NO period column:
--     the cursor is period-INDEPENDENT (state-guardian C9 / SC-4). A period
--     column would re-break the "no reset at rollover" invariant.
-- (c) metering_config — single-row kill-switch, seeded DISABLED. Observe-only
--     ships off; an operator flips `enabled` true after LIVE validation (EC-5).
--
-- RLS: metering_cursor + metering_config carry NO row-level-security policy by
-- design. They are control-plane tables (not tenant-facing). The metering worker
-- runs as the non-owner loopruntime_worker role (RLS applies to it on tenant tables),
-- and it scans cross-tenant here; leaving these two tables without an RLS policy
-- keeps them unfiltered for the worker. Cross-tenant billing writes into
-- cost_counters / usage_records still go through `SET LOCAL app.tenant_id = ''`
-- (those tables' RLS is bypassed that way — migration 000023).

ALTER TABLE cost_counters DROP CONSTRAINT cost_counters_metric_check;
ALTER TABLE cost_counters ADD CONSTRAINT cost_counters_metric_check
    CHECK (metric IN ('egress_bytes', 'docr_pulls', 'build_minutes', 'cpu_core_seconds', 'mem_gb_seconds'));

CREATE TABLE IF NOT EXISTS metering_cursor (
    tenant_id       TEXT NOT NULL,
    metric          TEXT NOT NULL,
    cell_id         TEXT NOT NULL,
    last_metered_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, metric, cell_id)
);

CREATE TABLE IF NOT EXISTS metering_config (
    id      BOOLEAN PRIMARY KEY DEFAULT true,
    enabled BOOLEAN NOT NULL DEFAULT false,
    CONSTRAINT metering_config_singleton CHECK (id = true)
);
INSERT INTO metering_config (id, enabled) VALUES (true, false) ON CONFLICT (id) DO NOTHING;

GRANT SELECT, INSERT, UPDATE ON metering_cursor TO loopruntime_worker;
GRANT SELECT ON metering_config TO loopruntime_worker;
