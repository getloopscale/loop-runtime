-- Migration 000015: Fix subscriptions constraints for billing provisioner
--
-- The provisioner needs ON CONFLICT (tenant_id) DO NOTHING, which requires
-- a UNIQUE constraint on tenant_id (index alone is not enough for PostgreSQL).
-- Also relax NOT NULL on stripe_customer_id and stripe_subscription_id
-- because starter (free) plans are DB-only with no Stripe customer.

-- Add unique constraint on tenant_id (one subscription per tenant)
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_tenant_id_unique UNIQUE (tenant_id);

-- Allow empty stripe IDs for free tier (starter plan = DB-only, no Stripe)
ALTER TABLE subscriptions ALTER COLUMN stripe_customer_id DROP NOT NULL;
ALTER TABLE subscriptions ALTER COLUMN stripe_subscription_id DROP NOT NULL;

-- Drop the UNIQUE on stripe_subscription_id to allow multiple NULLs (free tier)
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_stripe_subscription_id_key;
