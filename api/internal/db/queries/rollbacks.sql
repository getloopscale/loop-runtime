-- queries/rollbacks.sql: the record of each rollback (migration 000070).
--
-- One row per rollback, inserted when it starts and closed once, when it ends.
-- Every query carries the tenant, in addition to row level security.

-- name: InsertRollback :exec
-- Runs on the transaction that moves the active release to ROLLING_BACK, so a
-- rollback that started always has its record.
INSERT INTO rollbacks (
  id, tenant_id, project_id, environment,
  from_release_id, from_build_id, to_release_id, to_build_id,
  created_by, correlation_id
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
);

-- name: FinishRollback :execrows
-- Closes a rollback that is still in progress. A second close changes nothing:
-- the first outcome recorded is the one that stands.
UPDATE rollbacks
SET status = $3, failure_reason = $4, finished_at = now()
WHERE id = $1 AND tenant_id = $2 AND status = 'IN_PROGRESS';

-- name: ListRollbacksByProject :many
SELECT * FROM rollbacks
WHERE tenant_id = $1 AND project_id = $2
ORDER BY created_at DESC
LIMIT $3;

-- name: LastRollbackTo :one
-- When the last rollback that ended on this release finished, which is when
-- the release became active again. NULL when no rollback ended on it.
SELECT max(finished_at)::timestamptz AS finished_at
FROM rollbacks
WHERE tenant_id = $1 AND to_release_id = $2 AND status = 'SUCCEEDED';
