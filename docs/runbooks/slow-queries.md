# Runbook: Slow Query Diagnosis with pg_stat_statements

## Severity / Priority

**P2 — High.** Slow queries degrade API latency, increase advisory lock hold times, and can cascade into pool exhaustion (see [Pool Exhaustion runbook](./pool-exhaustion.md)). Deploy latency above 5s for DB operations indicates a problem.

## Symptoms

- `loopzero deploy` or `loopzero rollback` takes significantly longer than usual.
- Grafana dashboards show increased `loopzero_http_request_duration_seconds` on DB-backed endpoints.
- `loopzero_lock_hold_duration_seconds` histogram shifts right (locks held longer because queries inside locks are slow).
- `loopzero_postgres_connections_active` is elevated but not at max — connections are busy, not leaked.
- PostgreSQL logs show `LOG: duration: XXXX.XXX ms` entries (if `log_min_duration_statement` is configured).

## Diagnosis

### 1. Enable pg_stat_statements (if not already)

Check if the extension is installed:

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "SELECT * FROM pg_stat_statements LIMIT 1;" 2>&1
```

If the table does not exist, enable it:

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "CREATE EXTENSION IF NOT EXISTS pg_stat_statements;"
```

For DigitalOcean managed PostgreSQL, `pg_stat_statements` is enabled by default. For local k3d, ensure `shared_preload_libraries = 'pg_stat_statements'` is set in the PostgreSQL Helm values.

### 2. Top 10 slowest queries by total time

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    SELECT
      calls,
      round(total_exec_time::numeric, 2) AS total_ms,
      round(mean_exec_time::numeric, 2) AS mean_ms,
      round(max_exec_time::numeric, 2) AS max_ms,
      rows,
      left(query, 100) AS query
    FROM pg_stat_statements
    WHERE dbid = (SELECT oid FROM pg_database WHERE datname = 'loopzero')
    ORDER BY total_exec_time DESC
    LIMIT 10;
  "
```

### 3. Top 10 queries by mean execution time

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    SELECT
      calls,
      round(mean_exec_time::numeric, 2) AS mean_ms,
      round(stddev_exec_time::numeric, 2) AS stddev_ms,
      rows,
      left(query, 100) AS query
    FROM pg_stat_statements
    WHERE dbid = (SELECT oid FROM pg_database WHERE datname = 'loopzero')
      AND calls > 10
    ORDER BY mean_exec_time DESC
    LIMIT 10;
  "
```

### 4. Top 10 queries by call frequency

High-frequency queries with even small inefficiencies compound:

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    SELECT
      calls,
      round(total_exec_time::numeric, 2) AS total_ms,
      round(mean_exec_time::numeric, 2) AS mean_ms,
      left(query, 100) AS query
    FROM pg_stat_statements
    WHERE dbid = (SELECT oid FROM pg_database WHERE datname = 'loopzero')
    ORDER BY calls DESC
    LIMIT 10;
  "
```

### 5. EXPLAIN ANALYZE a suspicious query

Once you identify a slow query from `pg_stat_statements`, get its full execution plan:

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
    <paste the full query here with sample parameter values>;
  "
```

**What to look for in the plan:**

| Pattern | Meaning | Fix |
|---------|---------|-----|
| `Seq Scan on builds` | Full table scan | Add index on filter columns |
| `Sort` with high `actual rows` | Sorting without index support | Add index matching ORDER BY |
| `Nested Loop` with high outer rows | O(n*m) join | Check join conditions, add indexes |
| `Hash Join` on large tables | Memory pressure | Increase `work_mem` or optimize query |
| `Buffers: shared read` much larger than `shared hit` | Cold cache or table too large for memory | Check index usage, consider partitioning |

### 6. Check for missing indexes

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    SELECT
      schemaname, relname AS table,
      seq_scan, seq_tup_read,
      idx_scan, idx_tup_fetch,
      n_live_tup AS row_count
    FROM pg_stat_user_tables
    WHERE seq_scan > 100
      AND n_live_tup > 1000
    ORDER BY seq_tup_read DESC
    LIMIT 10;
  "
```

Tables with high `seq_scan` and high `seq_tup_read` relative to `idx_scan` are candidates for new indexes.

## Mitigation Steps

### Step 1: Reset statistics to get a clean baseline

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "SELECT pg_stat_statements_reset();"
```

Wait for the slow behavior to recur, then re-run the top-N queries.

### Step 2: Add missing index

If diagnosis reveals a sequential scan on a frequently queried column, add an index via a forward-only migration:

```sql
-- api/internal/db/migrations/000011_add_missing_index.up.sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_<table>_<column>
  ON <table> (<column>);
```

**Important:** Use `CREATE INDEX CONCURRENTLY` to avoid locking the table during index creation. This cannot run inside a transaction, so it must be the only statement in its migration file.

### Step 3: Tune statement_timeout for safety

The API sets `statement_timeout` via pgx RuntimeParams. If runaway queries are observed, reduce the timeout:

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system set env deploy/loopzero-api \
  DB_STATEMENT_TIMEOUT=30000
```

This kills any query running longer than 30 seconds.

### Step 4: Investigate RLS overhead

RLS policies (migration 000009) add a `current_setting('app.tenant_id', true)` check to every query on `builds`, `releases`, `environment_targets`, and `secrets`. If RLS is causing significant overhead:

1. Check if the query plan shows `Filter: (tenant_id = ...)` applied efficiently (index on `tenant_id`).
2. The `builds`, `releases`, and `environment_targets` tables already have indexes on `tenant_id` (migration 000007). Verify they are being used.

## Verification

After applying a fix (new index, query rewrite, or timeout change):

1. Reset `pg_stat_statements`:
   ```bash
   kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
     psql -U loopzero -d loopzero -c "SELECT pg_stat_statements_reset();"
   ```

2. Run a representative workload (deploy, rollback, status).

3. Re-check the top-N queries — the previously slow query should show improved `mean_exec_time`.

4. Verify Grafana metrics:
   - `loopzero_http_request_duration_seconds` should decrease.
   - `loopzero_lock_hold_duration_seconds` should decrease.

## Prevention

1. **All tables must have indexes on frequently filtered columns.** Migration 000007 adds indexes for the known query patterns. New queries must be evaluated for index support.

2. **Statement timeout** is set as a safety net via pgx pool configuration (`StatementTimeout` field in `PoolConfig`). Never remove it.

3. **Advisory lock hold time** is the best proxy for DB health. If `loopzero_lock_hold_duration_seconds` increases, investigate queries inside the lock.

4. **Forward-only migrations** — never add indexes in a way that blocks writes. Always use `CREATE INDEX CONCURRENTLY`.

5. **Review new queries** — any new store method should include an `EXPLAIN ANALYZE` check during code review to confirm index usage.

## Related Alerts

| Alert | Condition |
|-------|-----------|
| `LoopZeroAPILatencyHigh` | p99 latency > 2s for 5 min |
| `PostgresPoolNearExhaustion` | Active connections > 80% of MaxConns for 2 min |
| `LoopZeroDeployFailureRate` | Deploy failure rate > 20% in 15 min window |
| `LoopZeroLockHoldDuration` | p95 lock hold time > 3s |
