# Query Plans — Hot Paths

MEET-2026-03-31-024 Phase 1 Track B.4

This document records EXPLAIN ANALYZE results for the hottest database queries.
Run after migration 000009 is applied and representative data is seeded.

## How to Run

```bash
# Connect to PostgreSQL
task db:shell

# For each query below, run:
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) <query>;
```

## Queries to Verify

### 1. ListByProject (build_store.go)

Used by: `loopzero status`, build list
Frequency: every user request

```sql
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT id, tenant_id, project_id, environment, status, correlation_id,
       created_at, created_by, started_at, finished_at, error_message,
       duration_seconds, COALESCE(commit_sha, ''), COALESCE(workflow_id, '')
FROM builds
WHERE tenant_id = 'test-tenant' AND project_id = 'test-project'
ORDER BY created_at DESC
LIMIT 20 OFFSET 0;
```

**Expected:** Index Scan using idx_builds_tenant_project

### 2. HasActiveBuild (build_store.go)

Used by: deploy (inside advisory lock)
Frequency: every deploy attempt

```sql
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT EXISTS(
  SELECT 1 FROM builds
  WHERE tenant_id = 'test-tenant' AND project_id = 'test-project'
    AND environment = 'production'
    AND status IN ('QUEUED', 'RUNNING', 'RELEASING')
);
```

**Expected:** Index Scan using idx_one_active_build_per_env (partial index)

### 3. GetActiveByEnv (release_store.go)

Used by: rollback, status, sync callback
Frequency: every deploy completion + status check

```sql
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT id, build_id, tenant_id, project_id, environment, status,
       correlation_id, created_at, created_by,
       COALESCE(git_commit, ''), COALESCE(git_mirror_status, ''),
       COALESCE(apps, 'null'::jsonb), COALESCE(artifacts, 'null'::jsonb),
       COALESCE(side_effects, 'null'::jsonb)
FROM releases
WHERE tenant_id = 'test-tenant' AND project_id = 'test-project'
  AND environment = 'production' AND status = 'ACTIVE'
ORDER BY created_at DESC LIMIT 1;
```

**Expected:** Index Scan using idx_releases_env

### 4. FindActiveBuilds (build_store.go — reconciliation)

Used by: build reconcile loop (every 30s)
Frequency: background, cross-tenant

```sql
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT id, tenant_id, project_id, environment, status, correlation_id,
       created_at, created_by, started_at, finished_at, error_message,
       duration_seconds, COALESCE(commit_sha, ''), COALESCE(workflow_id, '')
FROM builds
WHERE status IN ('QUEUED', 'RUNNING');
```

**Expected:** Index Scan using idx_one_active_build_per_env or Seq Scan (acceptable if < 100 active builds)

### 5. FindPendingReleases (release_store.go — convergence loop)

Used by: release convergence loop (every 30s)
Frequency: background, cross-tenant

```sql
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT id, build_id, tenant_id, project_id, environment, status,
       correlation_id, created_at, created_by,
       COALESCE(git_commit, ''), COALESCE(git_mirror_status, ''),
       COALESCE(apps, 'null'::jsonb), COALESCE(artifacts, 'null'::jsonb),
       COALESCE(side_effects, 'null'::jsonb)
FROM releases
WHERE status = 'PENDING'
ORDER BY created_at ASC;
```

**Expected:** Index Scan using idx_releases_pending (new in migration 000009)

### 6. GetLatestByBuildID (release_store.go)

Used by: build completion callback
Frequency: every build completion

```sql
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT id, build_id, tenant_id, project_id, environment, status,
       correlation_id, created_at, created_by,
       COALESCE(git_commit, ''), COALESCE(git_mirror_status, ''),
       COALESCE(apps, 'null'::jsonb), COALESCE(artifacts, 'null'::jsonb),
       COALESCE(side_effects, 'null'::jsonb)
FROM releases
WHERE build_id = 'test-build-id'
ORDER BY created_at DESC LIMIT 1;
```

**Expected:** Index Scan using idx_releases_build_id (new in migration 000009)

## Results

<!-- Fill after running against real database -->

| # | Query | Plan | Cost | Rows | Index Used | Status |
|---|-------|------|------|------|-----------|--------|
| 1 | ListByProject | — | — | — | — | PENDING |
| 2 | HasActiveBuild | — | — | — | — | PENDING |
| 3 | GetActiveByEnv | — | — | — | — | PENDING |
| 4 | FindActiveBuilds | — | — | — | — | PENDING |
| 5 | FindPendingReleases | — | — | — | — | PENDING |
| 6 | GetLatestByBuildID | — | — | — | — | PENDING |

## Acceptance Criteria

- All queries 1-3 (user-facing) MUST use index scans
- Queries 4-5 (background) SHOULD use index scans or acceptable seq scans (< 100ms)
- Query 6 MUST use idx_releases_build_id (new index)
- No seq scan on tables > 1K rows for user-facing queries
