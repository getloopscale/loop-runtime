-- Migration 000008: archive stale failed/cancelled releases.
--
-- fix-all-platform-issues-plan T2.3 / ADR D5 (release pipeline diagnostics).
--
-- State at audit time (docs/audits/failed-releases-2026-05-02.md):
--   * 70 CANCELLED releases (operator-cancelled during dev iterations)
--   * 8 GIT_MIRROR_FAILED (specific to repotest project, 2026-04-29 06:xx)
--   * 3 SYNC_FAILED (across multiple projects, same date)
--   * 0 ACTIVE
--
-- Adding 'ARCHIVED' as a terminal status that excludes records older than
-- 7 days from active query paths. Audit trail is preserved (records remain
-- in the table). Real fixes for the underlying bugs are in T4.1 + T4.2.
--
-- Idempotent: WHERE clause filters status + age, so re-running flips zero
-- additional rows after the first apply on a given timestamp.
--
-- Forward-only (CLAUDE.md Rule). To revert a specific record from
-- 'ARCHIVED' back to its prior status, requires a NEW migration with
-- explicit row IDs.

-- 1. Append 'ARCHIVED' to the release status CHECK constraint.
-- The constraint currently lists CANCELLED, GIT_MIRROR_FAILED, etc.
-- We re-create it via DROP+ADD because PG doesn't support adding values
-- to an existing CHECK without rewrite.
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'releases_status_check'
  ) THEN
    ALTER TABLE releases DROP CONSTRAINT releases_status_check;
  END IF;
END$$;

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
    'ARCHIVED'
  ));

-- 2. Archive stale failed/cancelled releases.
UPDATE releases
   SET status = 'ARCHIVED',
       side_effects = COALESCE(side_effects, '{}'::jsonb) || jsonb_build_object(
         'archived_at', now()::text,
         'archived_reason', 'fix-all-platform-issues-plan T2.3 — stale > 7 days',
         'archived_from_status', status
       )
 WHERE status IN ('CANCELLED', 'GIT_MIRROR_FAILED', 'SYNC_FAILED')
   AND created_at < now() - interval '7 days';

-- 3. Index for fast filtering of non-archived releases.
CREATE INDEX IF NOT EXISTS idx_releases_status_active
  ON releases (status) WHERE status != 'ARCHIVED';

COMMENT ON INDEX idx_releases_status_active IS
  'fix-all-platform-issues-plan T2.3 — exclude ARCHIVED rows from active list queries.';
