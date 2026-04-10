-- Enable pg_stat_statements for query performance visibility.
-- DO Managed PostgreSQL has the extension preloaded; this just activates it.
-- Dashboard: database-health.json "Top 10 Slow Queries" panel.
-- Runbook: check mean_exec_time, calls, rows for hot queries.
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
