-- 000010_release_rollback_states.up.sql
--
-- CHG-2026-05-05-DOGFOOD-B37: add ROLLING_BACK + ROLLED_BACK to the
-- releases.status CHECK constraint.
--
-- Background: pkg/statemachine/release.go declares both states (line
-- 27-28: `ReleaseRollingBack`, `ReleaseRolledBack`) as part of the
-- canonical Release state machine. Production code transitions to
-- ROLLING_BACK on `loopscale rollback` (services/rollback_orchestrator.go)
-- and to ROLLED_BACK after the new sync activates. The CHECK constraint
-- introduced in migration 000001 (and rewritten in 000008 for ARCHIVED)
-- never included these two states, so every rollback attempt fails
-- with: `new row for relation "releases" violates check constraint
-- "releases_status_check" (SQLSTATE 23514)` — discovered LIVE in
-- real-paas-cleanroom-dogfood-loop iter 28 journey-06.
--
-- Forward-only: this migration drops the existing CHECK and re-adds it
-- with the full canonical set (10 prior states + 2 new = 12 total).
-- No data migration required — current rows already use existing values.

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

COMMENT ON CONSTRAINT releases_status_check ON releases IS
  'CHG-2026-05-05-DOGFOOD-B37 — full canonical state set including rollback states defined by pkg/statemachine/release.go.';
