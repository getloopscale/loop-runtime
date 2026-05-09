-- 000013_audit_log_time_index.down.sql
DROP INDEX IF EXISTS idx_audit_log_created_at_brin;
