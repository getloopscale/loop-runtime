-- V9 L16 fix: remove hardcoded env whitelist on app_scaling_config.
-- Migration 000009 had:
--   environment TEXT NOT NULL CHECK (environment IN ('production', 'staging', 'preview'))
-- This blocked any tenant-created env (j-scale, j-solo, dev-1, qa, etc.) at
-- the DB layer, even after L13 removed the Go-side validator.
--
-- New constraint enforces DNS-label syntax only:
--   - lowercase alphanumeric + dash
--   - 1-63 chars (DNS label limit)
--
-- LoopRuntime's product surface supports tenant-created envs. Existence is enforced
-- by withEnvironmentLock at route level; the DB CHECK should only enforce
-- syntactic correctness for FK-friendly storage.
--
-- Forward-only migration (LoopRuntime convention).

ALTER TABLE app_scaling_config
  DROP CONSTRAINT IF EXISTS app_scaling_config_environment_check;

ALTER TABLE app_scaling_config
  ADD CONSTRAINT app_scaling_config_environment_syntax_check
  CHECK (
    environment ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'
  );
