-- 000011_release_check_constraint_canonical.up.sql
--
-- T0.1 of deep-review-remediation-plan.md (closes find_data_001 CRITICAL +
-- find_data_002 CRITICAL + threat 8).
--
-- BACKGROUND
-- ----------
-- Migration 000010 (B37 fix) restored ROLLING_BACK + ROLLED_BACK to the
-- releases.status CHECK constraint but missed two other live states from
-- pkg/statemachine/release.go:
--   * DRIFT_ALERT      — written by ReleaseSyncExhausted event chain
--   * ROLLBACK_FAILED  — written by services/rollback_orchestrator.go:271
--                        when rollback delivery fails (Gitea down, ArgoCD
--                        timeout, git push failure, etc.)
--
-- Additionally, migration 000008 (DROP+RECREATE for ARCHIVED) introduced
-- two phantom states that NO state machine declares:
--   * BUILDING — never produced by any Transition()
--   * FAILED   — never produced by any Transition()
--
-- These phantom values create a data corruption surface: any code path or
-- manual SQL that writes 'BUILDING' or 'FAILED' produces rows that the
-- runtime cannot read back without state machine panic.
--
-- BEFORE STATE (post-migration 000010):
--   PENDING, BUILDING, READY, ACTIVE, SUPERSEDED, CANCELLED,
--   GIT_MIRROR_FAILED, SYNC_FAILED, FAILED, ARCHIVED,
--   ROLLING_BACK, ROLLED_BACK
--
-- AFTER STATE (this migration):
--   PENDING, READY, ACTIVE, SUPERSEDED, CANCELLED,
--   GIT_MIRROR_FAILED, SYNC_FAILED, DRIFT_ALERT, ARCHIVED,
--   ROLLING_BACK, ROLLED_BACK, ROLLBACK_FAILED
--
-- Net change: -BUILDING -FAILED +DRIFT_ALERT +ROLLBACK_FAILED.
--
-- ADR D2 (forward-only, no DROP+RECREATE without state machine cross-check):
-- this migration explicitly enumerates BEFORE and AFTER. The accompanying
-- Go test TestStateMachineCheckConstraintInvariant (api/tests/integration/)
-- asserts pg_constraint.consrc matches the AFTER set exactly, preventing
-- future drift via reflection on pkg/statemachine/release.go constants.
--
-- EC-1 (deep-review-remediation edge case review): pre-migration audit guard.
-- If any rows exist with the phantom values, this migration MUST fail loud
-- before applying ALTER TABLE. Otherwise PG would acquire ACCESS EXCLUSIVE
-- and then fail the validation step, leaving the table in an opaque locked
-- state with a generic 23514 error. We surface the root cause explicitly.

-- =============================================================================
-- Step 1 — pre-migration audit (EC-1 fix)
-- =============================================================================
DO $$
DECLARE
  phantom_count INTEGER;
BEGIN
  SELECT COUNT(*) INTO phantom_count
  FROM releases
  WHERE status IN ('BUILDING', 'FAILED');

  IF phantom_count > 0 THEN
    RAISE EXCEPTION
      'Migration 000011 aborted: % releases row(s) have phantom states (BUILDING or FAILED) that this migration is dropping from the CHECK constraint. Run a cleanup migration first to either DELETE these rows or remap their status to a canonical value.',
      phantom_count;
  END IF;
END $$;

-- =============================================================================
-- Step 2 — drop existing constraint
-- =============================================================================
ALTER TABLE releases DROP CONSTRAINT IF EXISTS releases_status_check;

-- =============================================================================
-- Step 3 — add canonical CHECK constraint matching pkg/statemachine/release.go
-- =============================================================================
ALTER TABLE releases
  ADD CONSTRAINT releases_status_check
  CHECK (status IN (
    -- Live states from pkg/statemachine/release.go
    'READY',
    'PENDING',
    'ACTIVE',
    'GIT_MIRROR_FAILED',
    'SYNC_FAILED',
    'DRIFT_ALERT',          -- ADDED in 000011 (closes find_data_001)
    'SUPERSEDED',
    'ROLLING_BACK',
    'ROLLED_BACK',
    'ROLLBACK_FAILED',      -- ADDED in 000011 (closes find_data_001)
    'CANCELLED',
    -- Archive-only terminal state (added by 000008 for archive feature;
    -- not in pkg/statemachine but valid persisted-only metadata)
    'ARCHIVED'
  ));

COMMENT ON CONSTRAINT releases_status_check ON releases IS
  'deep-review-remediation T0.1 — canonical state set matching pkg/statemachine/release.go + ARCHIVED. Drift is enforced by TestStateMachineCheckConstraintInvariant.';
