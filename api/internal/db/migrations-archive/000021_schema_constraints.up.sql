-- Migration 000021: Add CHECK constraints for enum-like columns
-- PLAN-auth-billing-hardening §2.4

-- Email uniqueness (one account per email)
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_unique ON users (email);

-- Role validation
ALTER TABLE tenant_members ADD CONSTRAINT chk_tenant_members_role
  CHECK (role IN ('owner', 'admin', 'member'));

-- Plan validation (subscriptions is the single source of truth since §1.1)
ALTER TABLE subscriptions ADD CONSTRAINT chk_subscriptions_plan
  CHECK (plan IN ('starter', 'pro', 'team', 'enterprise'));

-- Subscription status validation (matches Stripe subscription states)
ALTER TABLE subscriptions ADD CONSTRAINT chk_subscriptions_status
  CHECK (status IN ('active', 'past_due', 'canceled', 'incomplete', 'trialing', 'unpaid'));
