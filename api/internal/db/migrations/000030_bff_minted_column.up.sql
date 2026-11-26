-- 000030_bff_minted_column.up.sql
-- Plan: dashboard-memory-bff-server-side-key-vault v1.2 § T0.1
--
-- Adds bff_minted column to loop_runtime_api_keys distinguishing BFF auto-minted keys
-- (created server-side by the Dashboard BFF vault) from user-minted keys
-- (created via POST /v1/api-keys by an authenticated dashboard user).
--
-- Partial index supports the cleanup-cron hot path:
--   SELECT id FROM loop_runtime_api_keys WHERE bff_minted=true AND expires_at < NOW()
--
-- Per ADR D4 of the plan: column over separate table (no JOIN on RequireScope
-- hot path; single bit flip).

ALTER TABLE loop_runtime_api_keys ADD COLUMN bff_minted BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX idx_loop_runtime_api_keys_bff_minted_expires
  ON loop_runtime_api_keys(bff_minted, expires_at)
  WHERE bff_minted = true;
