-- Migration 000018: Remove tenants.plan — subscriptions is the single source of truth
-- PLAN-auth-billing-hardening §1.1
--
-- Problem: tenants.plan, subscriptions.plan, and plan_limits.plan create three
-- potential answers to "what plan is this tenant on?"
--
-- Fix: subscriptions table is the single source of truth.
-- tenants.plan is removed. If no subscription row exists, code defaults to 'starter'.

-- Ensure every tenant has a subscription row before dropping the column
INSERT INTO subscriptions (id, tenant_id, plan, status, created_at, updated_at)
SELECT
    'sub_migrate_' || t.id,
    t.id,
    COALESCE(t.plan, 'starter'),
    'active',
    now(),
    now()
FROM tenants t
WHERE NOT EXISTS (
    SELECT 1 FROM subscriptions s WHERE s.tenant_id = t.id
);

-- Now safe to drop the column
ALTER TABLE tenants DROP COLUMN IF EXISTS plan;
