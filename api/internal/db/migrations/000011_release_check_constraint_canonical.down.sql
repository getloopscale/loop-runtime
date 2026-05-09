-- 000011_release_check_constraint_canonical.down.sql
--
-- Reverts to the migration 000010 state set: re-introduces phantom BUILDING
-- and FAILED, removes DRIFT_ALERT and ROLLBACK_FAILED.
--
-- WARNING: rolling back this migration re-opens find_data_001 + find_data_002.
-- Production rollback delivery failures will SQLSTATE 23514 again. Only use
-- if migration 000011 itself is broken (which would require fixing in 000012).

ALTER TABLE releases DROP CONSTRAINT IF EXISTS releases_status_check;

ALTER TABLE releases
  ADD CONSTRAINT releases_status_check
  CHECK (status IN (
    'PENDING',
    'BUILDING',
    'READY',
    'ACTIVE',
    'SUPERSEDED',
    'CANCELLED',
    'GIT_MIRROR_FAILED',
    'SYNC_FAILED',
    'FAILED',
    'ARCHIVED',
    'ROLLING_BACK',
    'ROLLED_BACK'
  ));
