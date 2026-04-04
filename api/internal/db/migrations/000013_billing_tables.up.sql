-- Migration 000013: Billing tables for Stripe integration (Sprint 11, Phase 5)
--
-- Three tables:
--   subscriptions: mirrors Stripe subscription state per tenant
--   usage_records: tracks metered usage (builds, storage, minutes)
--   plan_limits: configurable limits per plan per metric
--
-- Forward-only migration.

-- Subscriptions — one per tenant, mirrors Stripe state
CREATE TABLE IF NOT EXISTS subscriptions (
    id                   TEXT PRIMARY KEY,
    tenant_id            TEXT NOT NULL REFERENCES tenants(id),
    stripe_customer_id   TEXT NOT NULL,
    stripe_subscription_id TEXT NOT NULL UNIQUE,
    plan                 TEXT NOT NULL DEFAULT 'starter',
    status               TEXT NOT NULL DEFAULT 'active',
    current_period_start TIMESTAMPTZ,
    current_period_end   TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_tenant ON subscriptions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_stripe_customer ON subscriptions(stripe_customer_id);

-- Usage records — per tenant, per metric, per billing period
CREATE TABLE IF NOT EXISTS usage_records (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    metric          TEXT NOT NULL,
    quantity        INTEGER NOT NULL DEFAULT 0,
    period_start    TIMESTAMPTZ NOT NULL,
    period_end      TIMESTAMPTZ NOT NULL,
    reported_to_stripe BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_tenant_metric_period
    ON usage_records(tenant_id, metric, period_start);

-- Plan limits — configurable per plan per metric
CREATE TABLE IF NOT EXISTS plan_limits (
    plan        TEXT NOT NULL,
    metric      TEXT NOT NULL,
    hard_limit  INTEGER NOT NULL,
    soft_limit  INTEGER,
    PRIMARY KEY (plan, metric)
);

-- Seed default plan limits
INSERT INTO plan_limits (plan, metric, hard_limit, soft_limit) VALUES
    -- Starter (free)
    ('starter', 'projects', 2, NULL),
    ('starter', 'services_per_project', 3, NULL),
    ('starter', 'environments', 1, NULL),
    ('starter', 'builds_per_month', 100, NULL),
    ('starter', 'seats', 1, NULL),
    ('starter', 'secrets_per_env', 10, NULL),
    ('starter', 'ram_mb', 512, NULL),
    ('starter', 'vcpu_millicores', 500, NULL),
    ('starter', 'build_timeout_seconds', 600, NULL),
    ('starter', 'api_requests_per_min', 60, NULL),
    -- Pro ($19/seat/mo)
    ('pro', 'projects', 10, NULL),
    ('pro', 'services_per_project', 10, NULL),
    ('pro', 'environments', 3, NULL),
    ('pro', 'builds_per_month', 1000, NULL),
    ('pro', 'seats', 5, NULL),
    ('pro', 'secrets_per_env', 50, NULL),
    ('pro', 'ram_mb', 2048, NULL),
    ('pro', 'vcpu_millicores', 2000, NULL),
    ('pro', 'build_timeout_seconds', 1800, NULL),
    ('pro', 'api_requests_per_min', 300, NULL),
    -- Team ($49/seat/mo)
    ('team', 'projects', 999999, 50),
    ('team', 'services_per_project', 999999, 20),
    ('team', 'environments', 10, NULL),
    ('team', 'builds_per_month', 999999, 5000),
    ('team', 'seats', 20, NULL),
    ('team', 'secrets_per_env', 200, NULL),
    ('team', 'ram_mb', 8192, NULL),
    ('team', 'vcpu_millicores', 8000, NULL),
    ('team', 'build_timeout_seconds', 3600, NULL),
    ('team', 'api_requests_per_min', 1000, NULL),
    -- Enterprise (unlimited — enforced at application level, not DB)
    ('enterprise', 'projects', 999999, NULL),
    ('enterprise', 'services_per_project', 999999, NULL),
    ('enterprise', 'environments', 999999, NULL),
    ('enterprise', 'builds_per_month', 999999, NULL),
    ('enterprise', 'seats', 999999, NULL),
    ('enterprise', 'secrets_per_env', 999999, NULL),
    ('enterprise', 'ram_mb', 999999, NULL),
    ('enterprise', 'vcpu_millicores', 999999, NULL),
    ('enterprise', 'build_timeout_seconds', 7200, NULL),
    ('enterprise', 'api_requests_per_min', 5000, NULL)
ON CONFLICT DO NOTHING;

-- Webhook events — idempotency tracking for Stripe webhooks
CREATE TABLE IF NOT EXISTS webhook_events (
    event_id     TEXT PRIMARY KEY,
    event_type   TEXT NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_webhook_events_type ON webhook_events(event_type);

COMMENT ON TABLE subscriptions IS 'Mirrors Stripe subscription state per tenant';
COMMENT ON TABLE usage_records IS 'Tracks metered usage (builds, storage, minutes) per billing period';
COMMENT ON TABLE plan_limits IS 'Configurable limits per plan per metric (seeded with defaults)';
COMMENT ON TABLE webhook_events IS 'Idempotency tracking for Stripe webhook events';
