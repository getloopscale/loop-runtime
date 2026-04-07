-- Migration 000022: Document user_identity_map as deprecated
-- PLAN-auth-billing-hardening §5.2
--
-- user_identity_map is a transitional table for lazy migration of legacy GitHub-auth
-- users to Kratos identities. users.id IS the Kratos UUID for all new users.
-- This table will be dropped after all legacy users are migrated (target: 2026-Q3).
--
-- No schema change — this migration documents the deprecation intent.
COMMENT ON TABLE user_identity_map IS 'DEPRECATED (§5.2): Transitional bridge for legacy GitHub-auth → Kratos. Target removal: 2026-Q3.';
