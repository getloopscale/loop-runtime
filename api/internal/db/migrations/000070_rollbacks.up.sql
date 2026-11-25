-- A rollback is recorded as an operation of its own.
--
-- A rollback moved release states and left nothing that said it happened: who
-- asked, from which release to which, and how it ended. One that failed left
-- the environment as it was, so it was invisible everywhere but the API log.
-- Each rollback now has a row here, written when it starts and closed when it
-- ends:
--
--   IN_PROGRESS  the target's manifests are being delivered and rolled out
--   SUCCEEDED    the environment runs the target release
--   FAILED       the release rolled back from stayed active; failure_reason says why
--
-- The release and build ids are copied, with no foreign key: the row is history
-- and must outlive the releases it names. Deleting the tenant removes it.
--
-- @see api/internal/db/queries/rollbacks.sql

BEGIN;

CREATE TABLE IF NOT EXISTS rollbacks (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id      TEXT NOT NULL,
    environment     TEXT NOT NULL,
    from_release_id TEXT NOT NULL,
    from_build_id   TEXT NOT NULL,
    to_release_id   TEXT NOT NULL,
    to_build_id     TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'IN_PROGRESS'
                    CHECK (status IN ('IN_PROGRESS', 'SUCCEEDED', 'FAILED')),
    failure_reason  TEXT NOT NULL DEFAULT '',
    created_by      TEXT NOT NULL,
    correlation_id  TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at     TIMESTAMPTZ
);

-- The list of a project's rollbacks, newest first.
CREATE INDEX IF NOT EXISTS idx_rollbacks_project
    ON rollbacks (tenant_id, project_id, created_at DESC);

ALTER TABLE rollbacks ENABLE ROW LEVEL SECURITY;
ALTER TABLE rollbacks FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS rollbacks_tenant_isolation ON rollbacks;
CREATE POLICY rollbacks_tenant_isolation ON rollbacks
    USING      (tenant_id = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

COMMIT;
