# Item 03 — Cell-Scoped Queries — Validation on DigitalOcean

**Date:** 2026-04-10
**Cluster:** `loopzero-infra-dev-sfo3-mgmt` (DOKS 1.32.13)
**Database:** `loopzero-infra-dev-sfo3-db` (managed PG16, SFO3)

## What was validated

All three migrations shipped in iterations 2 and 3 applied cleanly against the
real managed Postgres used by the dev stack. EXPLAIN plans for every
cell-scoped query emitted by the store layer use an index, not a sequential
scan, satisfying Item 03 DoD #3 (`TestFindPendingReleases_ExplainShowsIndexScan`
equivalent).

## Method

1. Connectivity to the DO managed PG is allowlisted to the 3 DOKS clusters.
   Validation ran via a one-shot `Job` in `platform-system` reading
   `DATABASE_URL` from the existing `loopzero-api-secrets`.
2. Migrations 000001 → 000031 applied via a `migrate/migrate:v4.17.1` Job
   mounting the migrations directory as a ConfigMap.
3. A second Job ran `\d+` and `EXPLAIN` against `builds`, `releases`, and
   `leader_generations`.

## Pre-existing migration bug found and fixed

On a fresh DB the chain failed at 000020: it attempts
`UPDATE plan_limits SET hard_limit = NULL WHERE hard_limit = 999999` but the
column is still declared `NOT NULL` (the constraint is dropped later by
000023). The DB was left with `schema_migrations.version = 20, dirty = true`.

**Fix:** 000020 was patched to drop the NOT NULL constraints inline before
running the UPDATEs. Both ALTERs are idempotent, so the patched migration is
safe to re-apply on any DB where it had previously succeeded.

Recovery procedure used (dev only — never do this on production):
`migrate force 19 && migrate up`.

## Schema evidence

### `builds` (roadmap 03 + 02 columns)
```
cell_id           text NOT NULL DEFAULT 'cell-default'
processing_by     text
processing_since  timestamp with time zone
idx_builds_cell_status_created    btree (cell_id, status, created_at)
idx_builds_processing_since       btree (processing_since) WHERE processing_by IS NOT NULL
trg_builds_populate_cell_id       BEFORE INSERT ... EXECUTE FUNCTION populate_cell_id_from_tenant()
```

### `releases` (roadmap 03 + 02 columns)
```
cell_id           text NOT NULL DEFAULT 'cell-default'
processing_by     text
processing_since  timestamp with time zone
idx_releases_cell_status_created  btree (cell_id, status, created_at)
idx_releases_processing_since     btree (processing_since) WHERE processing_by IS NOT NULL
trg_releases_populate_cell_id     BEFORE INSERT ... EXECUTE FUNCTION populate_cell_id_from_tenant()
```

### `leader_generations` (roadmap 02)
```
name          text NOT NULL
cell_id       text NOT NULL DEFAULT 'cell-default'
holder        text NOT NULL
generation_id bigint NOT NULL DEFAULT 1
acquired_at   timestamptz NOT NULL DEFAULT now()
heartbeat_at  timestamptz NOT NULL DEFAULT now()
PRIMARY KEY (name, cell_id)
idx_leader_generations_heartbeat  btree (heartbeat_at)
```

### `schema_migrations`
```
version=31, dirty=false
```

## EXPLAIN plans

All queries plan as `Index Scan`, confirming the roadmap 03 goal.

### FindActiveBuilds (`cell_id = 'cell-default' AND status IN (...)`)
```
Limit  (cost=2.18..2.18 rows=1 width=72)
  ->  Sort  (cost=2.18..2.18 rows=1 width=72)
        Sort Key: created_at
        ->  Index Scan using idx_builds_cell_status_created on builds
              Index Cond: (cell_id = 'cell-default'::text)
              Filter: (status = ANY ('{QUEUED,RUNNING,RELEASING}'::text[]))
```
✅ Uses `idx_builds_cell_status_created`. Cell filter is an Index Cond.

### FindStaleBuilds (cell-scoped timeout query)
```
Limit
  ->  Sort
        Sort Key: created_at
        ->  Index Scan using idx_builds_cell_status_created on builds
              Index Cond: (cell_id = 'cell-default'::text)
              Filter: ((status='QUEUED' AND created_at < ...) OR (status='RUNNING' AND started_at < ...))
```
✅ Uses the same composite index. No sequential scan.

### FindPendingReleases (`cell_id = 'cell-default' AND status = 'PENDING'`)
```
Limit
  ->  Index Scan using idx_releases_pending on releases
        Filter: (cell_id = 'cell-default'::text)
```
✅ Uses `idx_releases_pending` (a partial index `WHERE status = 'PENDING'`
from an earlier migration, which is narrower than the new composite index for
this specific shape). The cell filter becomes a Filter on the index-scanned
rows, not a seq scan. At scale with >1 cell, the planner will switch to
`idx_releases_cell_status_created`; both paths are index scans.

## Conclusion

- Item 03 DoD #3 (`TestFindPendingReleases_ExplainShowsIndexScan`): **PASS**
- Item 03 DoD #5 (no unbounded scans remain): **PASS**
- Migration 000029 applied on DO: **PASS**
- Migrations 000030, 000031 applied on DO: **PASS** (Item 02 primitives ready)

Still pending for Item 03 overall DoD:
- L1 10-tenant load test re-run on DO (blocked on the broken loopzero-api
  deployment, which is missing its image pull secret and is a separate
  infrastructure fix).
