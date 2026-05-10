-- 000015_build_apps_index.up.sql
--
-- T3.2 of deep-review-2026-05-10-remediation-plan
-- (closes find_data_missing_fk_index HIGH).
--
-- BACKGROUND
-- ----------
-- `build_apps` has FK `build_id REFERENCES builds(id) ON DELETE CASCADE`
-- but NO index on `build_id`. Every `GetBuildApps(buildID)` call does
-- Seq Scan + filter — fine at 100 builds, problematic at 10k+.
--
-- More dangerous: `ON DELETE CASCADE` requires the child column to be
-- indexed for efficient delete propagation. Without the index, deleting
-- a build with many `build_apps` rows triggers a full table scan on the
-- child every time. This is a known PG performance pitfall documented
-- in the FK-index correlation heuristic.

CREATE INDEX IF NOT EXISTS idx_build_apps_build_id
  ON build_apps (build_id);

COMMENT ON INDEX idx_build_apps_build_id IS
  'deep-review-2026-05-10 T3.2 — required for FK cascade delete + GetBuildApps query.';
