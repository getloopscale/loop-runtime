-- 000013_audit_log_time_index.up.sql
--
-- T7.3 of deep-review-remediation-plan (closes find_data_005 MEDIUM partial).
--
-- BACKGROUND
-- ----------
-- audit_log is append-only via trg_audit_log_immutable trigger but has NO
-- time-based index. Retention queries ("delete rows older than 1 year")
-- require a sequential scan, which becomes prohibitive once the table
-- grows past a few million rows. Even reading "audit events from
-- yesterday" needs a Seq Scan today.
--
-- Adds a BRIN index on created_at — cheap to maintain (BRIN stores per-block
-- min/max only) and ideal for append-only time-series data where rows are
-- naturally ordered by creation time.
--
-- BTREE alternatives: not chosen because the table grows monotonically
-- and the read pattern is "events in time range" not "lookup by exact
-- timestamp". BRIN is ~1000x smaller for this access pattern.
--
-- Retention policy itself is documented in ADR-0047 (audit_log retention).
-- The actual prune job is operational work tracked separately — this
-- migration enables the index it will need.

CREATE INDEX IF NOT EXISTS idx_audit_log_created_at_brin
  ON audit_log USING BRIN (created_at)
  WITH (pages_per_range = 32);

COMMENT ON INDEX idx_audit_log_created_at_brin IS
  'deep-review-remediation T7.3 — enables time-range retention scans without seq scan.';
