-- queries/builds.sql — sqlc query definitions for the builds table.
--
-- T1.2 of plan 2026-05-29-sqlc-migration-plan v1.3 (Phase 1
-- pilot — concurrent-build invariant Rule 6 + IDOR closure
-- find_sec_idor_buildstore + multi-cell cell_id propagation).
--
-- Per ADR-1 D2 (pgx/v5) + D3 (emit_methods_with_db_argument) + D6
-- (schema=schema.gen.sql) + EC-3 (snake_case JSON tags via sqlc.yaml).
--
-- Multi-tenant SELECT / UPDATE / DELETE carries `tenant_id` predicate per
-- platform-security-engineer condition 1 + M-1 + M-2 (enforced by
-- cmd/loopruntime-ops/internal/quality/tenantpredicate gate). Cross-tenant queries are marked
-- `-- tenantpredicate:exempt` IMMEDIATELY ABOVE `-- name:`.
--
-- Concurrent-build invariant (Rule 6 — pg_advisory_xact_lock):
-- HasActiveBuildForEnv runs inside the env lock; sqlctxprop gate validates
-- the tx-required form propagation.

-- name: InsertBuild :one
-- Idempotent on PK collision: 23505 unique_violation surfaces as
-- ErrDeployInProgress in the wrapper (matches legacy behavior). Status
-- defaults to BPhQueued (set by wrapper before invoking).
INSERT INTO builds (
  id,
  tenant_id,
  project_id,
  environment,
  status,
  correlation_id,
  created_by,
  commit_sha
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- tenantpredicate:exempt
-- name: GetBuildByID :one
-- Cross-tenant lookup. HTTP handlers MUST NOT call this — use
-- GetBuildByIDForTenant (T0.4 of deep-review-2026-05-10-remediation-plan
-- find_sec_idor_buildstore). Reserved for internal callers (convergence
-- loops, reaper, domain_provisioner) that authenticated via other gate.
SELECT * FROM builds WHERE id = $1;

-- name: GetBuildByIDForTenant :one
-- Tenant-scoped variant. Returns no row when build belongs to a different
-- tenant — closing the IDOR vector at the DB layer.
SELECT * FROM builds
WHERE id = $1 AND tenant_id = $2;

-- name: HasActiveBuildForEnv :one
-- Concurrent-deploy invariant (ARCH-002 + Rule 6). RELEASING included
-- because such builds are mid-release-creation; allowing a new deploy
-- would create two concurrent releases for the same env. Runs inside
-- the advisory lock transaction (sqlctxprop gate validates the tx form).
SELECT EXISTS(
  SELECT 1 FROM builds
  WHERE tenant_id = $1
    AND project_id = $2
    AND environment = $3
    AND status IN ('QUEUED', 'RUNNING', 'RELEASING')
);

-- name: UpdateBuildStatusToRunning :one
-- Transition QUEUED → RUNNING. Stamps started_at = NOW() so the duration
-- calculation at terminal transition has a baseline. Tenant predicate
-- preserved (stronger than legacy which UPDATE-ed by id alone).
UPDATE builds
SET status = $1,
    started_at = NOW()
WHERE id = $2 AND tenant_id = $3
RETURNING *;

-- name: UpdateBuildStatusToTerminal :one
-- Transition to SUCCEEDED/FAILED/CANCELLED. Stamps finished_at + computes
-- duration_seconds. COALESCE handles the corner case where started_at is
-- NULL (e.g., a QUEUED build cancelled before running) — duration = 0.
UPDATE builds
SET status = $1,
    finished_at = NOW(),
    duration_seconds = COALESCE(EXTRACT(EPOCH FROM (NOW() - started_at))::int, 0)
WHERE id = $2 AND tenant_id = $3
RETURNING *;

-- name: UpdateBuildStatusToRunningIfCurrent :one
-- CAS variant of UpdateBuildStatusToRunning (M9 5b, ADR 2026-07-03-m9-5b-nil-tx-status-atomicity).
-- Lock-free atomic QUEUED->RUNNING: applies ONLY if the row is still in the
-- expected `from` status ($4), closing the read-then-write window without a tx
-- or advisory lock. 0 rows => a concurrent lock-holder already advanced the
-- build => caller treats it as a superseded no-op. Tenant predicate preserved
-- (defense-in-depth; caller supplies req.TenantID).
UPDATE builds
SET status = $1,
    started_at = NOW()
WHERE id = $2 AND tenant_id = $3 AND status = $4
RETURNING *;

-- name: UpdateBuildStatusToTerminalIfCurrent :one
-- CAS variant of UpdateBuildStatusToTerminal (M9 5b). Lock-free atomic
-- transition to a terminal phase, applied ONLY if the row is still in the
-- expected `from` status ($4). COALESCE handles NULL started_at (duration=0).
-- 0 rows => superseded no-op.
UPDATE builds
SET status = $1,
    finished_at = NOW(),
    duration_seconds = COALESCE(EXTRACT(EPOCH FROM (NOW() - started_at))::int, 0)
WHERE id = $2 AND tenant_id = $3 AND status = $4
RETURNING *;

-- name: UpdateBuildStatusOther :one
-- Generic status update for non-timing-marker transitions (RELEASING,
-- CANCELLING). Tenant predicate preserved.
UPDATE builds
SET status = $1
WHERE id = $2 AND tenant_id = $3
RETURNING *;

-- name: SetBuildErrorMessage :execrows
-- Annotates the build with an error message and stamps finished_at. The
-- exec result's rows-affected tells the wrapper whether the build exists
-- (zero → ErrBuildNotFound). Tenant predicate preserved.
UPDATE builds
SET error_message = $1,
    finished_at = NOW()
WHERE id = $2 AND tenant_id = $3;

-- name: SetBuildWorkflowID :execrows
-- Records the Argo Workflow name for the build. Argo callback handler
-- reads back via GetBuildByID to drive reconcile (CODE-SCAN-001 path).
UPDATE builds
SET workflow_id = $1
WHERE id = $2 AND tenant_id = $3;

-- name: SetBuildAppsSpec :exec
-- Stores the canonicalized app spec (name/type/port/secrets/replicas/
-- autoscaling) at build submission time. The reconcile loop reads this
-- back via GetBuildApps to compute the deploy manifests.
UPDATE builds
SET apps_spec = $1::jsonb
WHERE id = $2 AND tenant_id = $3;

-- tenantpredicate:exempt
-- name: GetBuildApps :one
-- Returns the apps_spec JSON for a build. Cross-tenant by id (caller
-- validates ownership via GetBuildByIDForTenant before invoking, OR is
-- a reconcile path that authenticated via cell_id scope).
SELECT COALESCE(apps_spec, '[]'::jsonb) FROM builds WHERE id = $1;

-- name: ListBuildsByProject :many
-- Paginated list of builds for a (tenant, project) tuple, newest first.
SELECT * FROM builds
WHERE tenant_id = $1 AND project_id = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: ListBuildsByProjectAndEnv :many
-- Paginated list filtered by environment. The wrapper picks this OR
-- ListBuildsByProject based on whether env is empty.
SELECT * FROM builds
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
ORDER BY created_at DESC
LIMIT $4 OFFSET $5;

-- name: CountTenantBuildsSince :one
-- Plan enforcement: builds a tenant started since the given instant (the
-- start of the billing month), gated against plan_limits.builds_per_month.
SELECT COUNT(*)::int FROM builds
WHERE tenant_id = $1 AND created_at >= $2;

-- name: CountBuildsByProject :one
-- Total count for the project (used for pagination total in the wrapper).
SELECT COUNT(*) FROM builds
WHERE tenant_id = $1 AND project_id = $2;

-- name: CountBuildsByProjectAndEnv :one
-- Total count filtered by environment.
SELECT COUNT(*) FROM builds
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3;

-- tenantpredicate:exempt
-- name: FindActiveBuildsByCell :many
-- Cell-scoped reaper query (Roadmap 03 multi-cell). RELEASING included
-- per MEET-2026-03-26-008 (builds with completed images that need
-- release activation retried). Uses idx_builds_cell_status_created
-- (migration 000029).
SELECT * FROM builds
WHERE cell_id = $1
  AND status IN ('QUEUED', 'RUNNING', 'RELEASING')
ORDER BY created_at ASC
LIMIT 100;

-- name: CancelActiveBuildsByEnv :many
-- Bulk-cancel QUEUED/RUNNING builds during project deletion. Returns
-- workflow_ids for Argo cancellation (caller iterates + invokes Argo
-- API per row). Intentionally bypasses the state machine for batch ops
-- (RUNNING → CANCELLED skips CANCELLING intermediate; do NOT reuse for
-- single-build cancellation). Empty workflow_id replaced with '' for
-- the COALESCE pattern legacy preserved.
UPDATE builds
SET status = 'CANCELLED',
    finished_at = NOW()
WHERE tenant_id = $1
  AND project_id = $2
  AND environment = $3
  AND status IN ('QUEUED', 'RUNNING')
RETURNING COALESCE(workflow_id, '') AS workflow_id;

-- name: ListBuildEnvsForProject :many
-- Returns distinct environments the project has at least one build for,
-- ordered by most-recent build per env DESC. Feeds the dashboard
-- /deployments env-filter chips (ADR D3) — replaces the hardcoded
-- production/staging/preview/all literals so custom envs appear.
--
-- Per plan 2026-06-08-dashboard-screens-review-fixes T2.2 + EC-2:
-- handler MUST gate via ProjectStore.GetByName BEFORE this query
-- runs (cross-tenant existence-leak prevention). The query itself
-- is tenant-scoped (only this caller's tenant_id), but a tenant
-- could probe project names belonging to other tenants via the URL
-- path — the EC-2 adapter rejects with 404 PROJECT_NOT_FOUND before
-- this query is invoked.
--
-- Empty result → empty []string (not nil) per Rule 5 contract.
SELECT environment
FROM builds
WHERE tenant_id = $1 AND project_id = $2
GROUP BY environment
ORDER BY MAX(created_at) DESC;

-- name: BuildStatsByDay :many
-- J-06 / MEET-2026-07-03-003 D2: tenant-wide per-day build (deploy) counts for
-- the Overview deploys-by-day sparkline. Tenant isolation (#106/#111) — the
-- WHERE tenant_id = $1 predicate is the ONLY barrier under the BYPASSRLS engine
-- pool; NOT tenantpredicate:exempt so the CI gate enforces it. $2 is the window
-- lower bound (now() - window), computed in Go from the enumerated {7d,30d,90d}.
-- Day bucketing is server-side in UTC; the handler zero-fills missing days.
SELECT date_trunc('day', created_at AT TIME ZONE 'UTC')::date AS day, COUNT(*) AS count
FROM builds
WHERE tenant_id = $1
  AND created_at >= $2
GROUP BY day
ORDER BY day;
