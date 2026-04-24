-- Migration 000020: Use NULL for unlimited plan limits instead of 999999 sentinel
-- PLAN-auth-billing-hardening §1.5
--
-- NOTE (2026-04-10): This migration is structurally dependent on plan_limits
-- having nullable hard_limit/soft_limit columns. On fresh databases, 000013
-- seeds rows with `hard_limit = 999999 NOT NULL` and 000023 later drops the
-- NOT NULL constraint. This migration (000020) must run BETWEEN those two,
-- so it must drop the NOT NULL constraints itself. Without the ALTER, the
-- UPDATE fails the NOT NULL check and the entire migration chain is blocked
-- on any fresh install. Both ALTERs are idempotent: on any DB where 000020
-- has already applied (and 000023 subsequently dropped the constraints),
-- re-running these ALTERs is a no-op per PostgreSQL semantics.

-- Allow NULL in hard_limit and soft_limit (NULL = unlimited).
-- Idempotent: safe to re-run on databases where the constraint is already absent.
ALTER TABLE plan_limits ALTER COLUMN hard_limit DROP NOT NULL;
ALTER TABLE plan_limits ALTER COLUMN soft_limit DROP NOT NULL;

-- Replace sentinel value with NULL (NULL = unlimited)
UPDATE plan_limits SET hard_limit = NULL WHERE hard_limit = 999999;
UPDATE plan_limits SET soft_limit = NULL WHERE soft_limit = 999999;
