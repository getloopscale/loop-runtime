-- Migration 000031: Processing claim columns on builds and releases
-- Roadmap 02 — API Loop Extraction (MEET-2026-04-10-046)
--
-- Problem: BuildReconcileLoop and ReleaseConvergenceLoop today dedupe with
-- `sync.Map` held in-process. That map is lost the moment the controller is
-- extracted to a separate binary — two controller replicas would happily
-- process the same build at the same time.
--
-- Solution: PG-backed claim. Before a loop starts processing a build, it
-- writes `processing_by = <pod> AND processing_since = now()` via a
-- compare-and-swap UPDATE. The CAS predicate is
--   WHERE id = $1 AND (processing_by IS NULL OR processing_since < $stale)
-- so a live claim (younger than stale_after) blocks other workers, but an
-- orphaned claim (worker crashed mid-process) is recoverable after the
-- orphan timeout. The loop releases the claim in a deferred block or when
-- the build reaches a terminal state.
--
-- Forward-only: additive columns; safe to re-run because both ALTERs use
-- IF NOT EXISTS.

-- =============================================================================
-- builds.processing_by / processing_since
-- =============================================================================
ALTER TABLE builds
    ADD COLUMN IF NOT EXISTS processing_by    TEXT,
    ADD COLUMN IF NOT EXISTS processing_since TIMESTAMPTZ;

-- Partial index for orphan detection: only index rows that are actively
-- claimed, so the index stays small.
CREATE INDEX IF NOT EXISTS idx_builds_processing_since
    ON builds (processing_since)
    WHERE processing_by IS NOT NULL;

COMMENT ON COLUMN builds.processing_by IS
    'Pod/worker id of the controller currently processing this build. NULL when idle. Roadmap 02.';
COMMENT ON COLUMN builds.processing_since IS
    'Timestamp when processing_by claimed this build. Used to detect orphaned claims.';

-- =============================================================================
-- releases.processing_by / processing_since
-- =============================================================================
ALTER TABLE releases
    ADD COLUMN IF NOT EXISTS processing_by    TEXT,
    ADD COLUMN IF NOT EXISTS processing_since TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_releases_processing_since
    ON releases (processing_since)
    WHERE processing_by IS NOT NULL;

COMMENT ON COLUMN releases.processing_by IS
    'Pod/worker id of the controller currently processing this release. NULL when idle. Roadmap 02.';
COMMENT ON COLUMN releases.processing_since IS
    'Timestamp when processing_by claimed this release. Used to detect orphaned claims.';
