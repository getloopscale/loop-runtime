-- Migration 000020: Use NULL for unlimited plan limits instead of 999999 sentinel
-- PLAN-auth-billing-hardening §1.5

-- Replace sentinel value with NULL (NULL = unlimited)
UPDATE plan_limits SET hard_limit = NULL WHERE hard_limit = 999999;
UPDATE plan_limits SET soft_limit = NULL WHERE soft_limit = 999999;
