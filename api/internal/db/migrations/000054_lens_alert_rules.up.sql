-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000054_lens_alert_rules
--
-- loopruntime-lens M53 — per-tenant threshold alert rules on observability metrics.
-- A background evaluator (loopruntime-cloud lens_alert_evaluator) ticks periodically,
-- fetches each tenant's observability series via a broker-minted observability:read
-- key, evaluates each rule, and on a breach INSERTs a workspace_webhook_deliveries
-- row (dispatched SSRF-guarded by the existing J-16 workspace_webhook_worker).
--
-- Anti-storm: `status` (OK|TRIGGERED) + last_triggered_at drive a fire-once-until-
-- cleared state machine; the fire is atomic via `UPDATE ... WHERE status='OK'`.
--
-- Tenancy mirrors 000053_lens_saved_views: (subject, tenant_id) discriminator.
-- webhook_id references a customer-registered workspace_webhooks row (the only
-- target the reused delivery worker can POST to).
--
-- Owned/queried by loopruntime-cloud (raw database/sql); engine owns the schema.
-- Forward-only per CLAUDE.md convention.
-- ============================================================================

CREATE TABLE IF NOT EXISTS lens_alert_rules (
    id                        TEXT PRIMARY KEY,                                       -- lar-{16hex}
    subject                   TEXT NOT NULL,                                          -- auth.GetUser.Subject
    tenant_id                 TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name                      TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
    metric                    TEXT NOT NULL CHECK (metric IN ('error_rate', 'cost', 'p95_latency')),
    comparator                TEXT NOT NULL CHECK (comparator IN ('gt', 'gte', 'lt', 'lte')),
    threshold                 DOUBLE PRECISION NOT NULL,
    window_seconds            INTEGER NOT NULL CHECK (window_seconds > 0),
    webhook_id                TEXT NOT NULL REFERENCES workspace_webhooks(id) ON DELETE CASCADE,
    enabled                   BOOLEAN NOT NULL DEFAULT TRUE,
    -- Alert state (the anti-storm state machine).
    status                    TEXT NOT NULL DEFAULT 'OK' CHECK (status IN ('OK', 'TRIGGERED')),
    last_evaluated_at         TIMESTAMPTZ,
    last_triggered_at         TIMESTAMPTZ,
    renotify_interval_seconds INTEGER CHECK (renotify_interval_seconds IS NULL OR renotify_interval_seconds > 0),
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Backs the per-Principal list/delete scans (WHERE subject = $ AND tenant_id = $).
CREATE INDEX IF NOT EXISTS idx_lens_alert_rules_owner
    ON lens_alert_rules (tenant_id, subject);

-- Backs the evaluator's tenant fan-out (SELECT DISTINCT tenant_id ... WHERE enabled).
CREATE INDEX IF NOT EXISTS idx_lens_alert_rules_enabled
    ON lens_alert_rules (tenant_id) WHERE enabled;
