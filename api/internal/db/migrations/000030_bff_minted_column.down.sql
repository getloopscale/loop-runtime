-- 000030_bff_minted_column.down.sql
-- Plan: dashboard-memory-bff-server-side-key-vault v1.2 § T0.1
--
-- WARNING per EC-12: dev-only rollback. Production rollback with active BFF
-- rows MUST first delete distinguishable BFF rows via:
--   DELETE FROM loopruntime_api_keys WHERE key_name='bff-dashboard';
-- otherwise those rows lose their distinguishing marker and the cleanup cron
-- (in code, post-rollback) cannot find them.

DROP INDEX IF EXISTS idx_loopruntime_api_keys_bff_minted_expires;

ALTER TABLE loopruntime_api_keys DROP COLUMN bff_minted;
