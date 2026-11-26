-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000056_lens_fired_alerts
--
-- loopruntime-lens M84 — a queryable HISTORY of alert firings. Where 000054's
-- lens_alert_rules carries only a single last_triggered_at, this table records
-- ONE row per breach (metric value at fire time, comparator, threshold, when),
-- so an alert that fired is auditable in-app over time, not just "last fired".
--
-- The evaluator (loopruntime-cloud lens_alert_evaluator.processRule) INSERTs one row
-- here in the SAME transaction as the workspace_webhook_deliveries enqueue + the
-- status flip — so a breach yields exactly one delivery AND one history row,
-- atomically (either all three commit or none do).
--
-- `delivery_id` is a SOFT reference (plain text, NO FK) to the enqueued
-- workspace_webhook_deliveries row: the audit history MUST outlive delivery
-- retention (the worker reaps completed/failed deliveries), so a hard FK with
-- ON DELETE would corrupt the audit trail. `transition_id` is the dedup key the
-- delivery carries (alert-{ruleId}-{unixSecond}).
--
-- Tenancy mirrors 000054: tenant_id references tenants; rule_id references the
-- alert rule (cascade-deleted with it — history is meaningless without its rule).
--
-- Owned/queried by loopruntime-cloud (raw database/sql); engine owns the schema.
-- Forward-only per CLAUDE.md convention.
-- ============================================================================

CREATE TABLE IF NOT EXISTS lens_fired_alerts (
    id            TEXT PRIMARY KEY,                                             -- lfa-{16hex}
    rule_id       TEXT NOT NULL REFERENCES lens_alert_rules(id) ON DELETE CASCADE,
    tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    metric        TEXT NOT NULL CHECK (metric IN ('error_rate', 'cost', 'p95_latency')),
    comparator    TEXT NOT NULL CHECK (comparator IN ('gt', 'gte', 'lt', 'lte')),
    threshold     DOUBLE PRECISION NOT NULL,
    value         DOUBLE PRECISION NOT NULL,                                    -- the metric value at fire time
    fired_at      TIMESTAMPTZ NOT NULL,
    transition_id TEXT NOT NULL,                                                -- alert-{ruleId}-{unixSec} (delivery dedup key)
    delivery_id   TEXT NOT NULL,                                                -- soft ref to workspace_webhook_deliveries.id (no FK — history outlives delivery retention)
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Backs the paginated per-rule history read (WHERE rule_id = $ AND tenant_id = $ ORDER BY fired_at DESC).
CREATE INDEX IF NOT EXISTS idx_lens_fired_alerts_rule_time
    ON lens_fired_alerts (rule_id, tenant_id, fired_at DESC);

-- One history row per (rule, breach-second): the same anti-storm dedup as the delivery, so a
-- multi-replica evaluator writing the same transition can never double-record a firing.
CREATE UNIQUE INDEX IF NOT EXISTS uq_lens_fired_alerts_transition
    ON lens_fired_alerts (rule_id, transition_id);
