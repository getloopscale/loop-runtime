# Runbook: PostgreSQL Connection Pool Exhaustion

## Severity / Priority

**P1 — Critical.** Pool exhaustion blocks all API operations (deploys, rollbacks, status checks, secret reads). Every request that needs a DB connection will timeout and fail.

## Symptoms

- API returns HTTP 503 or timeouts on all endpoints.
- `loopzero deploy`, `loopzero rollback`, `loopzero status` fail with "deploy failure" (exit code 3) or generic errors.
- Grafana "Platform Overview" dashboard shows `loopzero_postgres_connections_active` pinned at 25 (the `MaxConns` default).
- `loopzero_postgres_connections_idle` drops to 0.
- Logs show `failed to acquire connection from pool` or `context deadline exceeded` from pgx.
- Advisory lock acquisitions fail with timeout (`loopzero_lock_conflicts_total` spikes).

## Diagnosis

### 1. Check pool metrics in Grafana

Open the **Platform Overview** or **Failure Investigation** dashboard.

| Metric | Healthy | Exhausted |
|--------|---------|-----------|
| `loopzero_postgres_connections_active` | < 15 | = 25 (MaxConns) |
| `loopzero_postgres_connections_idle` | > 5 | = 0 |
| `loopzero_postgres_connections_total` | 5-25 | = 25 |
| `loopzero_sse_connections_active` | < 50 | > 100 (possible cause) |

### 2. Check PostgreSQL directly

```bash
# Connect to the mgmt cluster
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    SELECT state, count(*)
    FROM pg_stat_activity
    WHERE datname = 'loopzero'
    GROUP BY state
    ORDER BY count DESC;
  "
```

Expected output when exhausted:

```
   state   | count
-----------+-------
 active    |    25
 idle      |     0
```

### 3. Identify what is holding connections

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    SELECT pid, state, wait_event_type, wait_event,
           age(now(), query_start) AS duration,
           left(query, 80) AS query
    FROM pg_stat_activity
    WHERE datname = 'loopzero'
      AND state != 'idle'
    ORDER BY query_start ASC;
  "
```

Look for:

- **Long-running queries** (duration > 30s) — possible missing index or unoptimized query.
- **`idle in transaction`** — leaked transaction (code opened tx but never committed/rolled back).
- **`Lock` in wait_event_type** — advisory lock contention (many concurrent deploys).
- **Many identical queries** — SSE polling or reconciliation loop gone wrong.

### 4. Check SSE connection count

SSE streams hold a DB connection for event journal LISTEN/NOTIFY. If clients reconnect aggressively, SSE can consume all pool slots.

```bash
# Check SSE connections
curl -s http://localhost:8080/metrics | grep loopzero_sse_connections_active
```

## Mitigation Steps

### Step 1: Kill idle-in-transaction connections (immediate relief)

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    SELECT pg_terminate_backend(pid)
    FROM pg_stat_activity
    WHERE datname = 'loopzero'
      AND state = 'idle in transaction'
      AND age(now(), state_change) > interval '60 seconds';
  "
```

### Step 2: Kill long-running queries

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    SELECT pg_terminate_backend(pid)
    FROM pg_stat_activity
    WHERE datname = 'loopzero'
      AND state = 'active'
      AND age(now(), query_start) > interval '5 minutes'
      AND pid <> pg_backend_pid();
  "
```

### Step 3: Increase MaxConns temporarily

Set the environment variable on the API deployment and restart:

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system set env deploy/loopzero-api \
  DB_MAX_CONNS=40

# Verify new pod is running
kubectl --context k3d-loopzero-mgmt -n loopzero-system rollout status deploy/loopzero-api
```

**Warning:** Ensure PostgreSQL `max_connections` (server-side) is higher than the new value. Default PostgreSQL max is 100. Check with:

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "SHOW max_connections;"
```

### Step 4: Identify and fix the leak

After immediate relief, find the root cause:

1. Search API logs for transactions that started but never committed:
   ```bash
   kubectl --context k3d-loopzero-mgmt -n loopzero-system logs deploy/loopzero-api --since=30m | \
     grep -i "begin\|commit\|rollback\|failed to acquire"
   ```

2. Check if SSE connections are the cause — if `loopzero_sse_connections_active` is high, the SSE reconnect logic may be broken or clients are not closing connections.

3. Check for slow queries — see the [Slow Queries runbook](./slow-queries.md).

## Verification

After mitigation, confirm recovery:

```bash
# Pool metrics should show headroom
curl -s http://localhost:8080/metrics | grep loopzero_postgres_connections

# Expected: active < MaxConns, idle > 0
# loopzero_postgres_connections_active 8
# loopzero_postgres_connections_idle 12
# loopzero_postgres_connections_total 20

# Functional check
loopzero status
# Should return exit code 0
```

## Prevention

1. **Pool configuration defaults** (in `api/internal/config/config.go`):
   - `DB_MAX_CONNS=25` — fits within PG default `max_connections=100` with room for operators and admin.
   - `DB_MIN_CONNS=5` — keeps warm connections to avoid cold-start latency.
   - `DB_MAX_CONN_IDLE_TIME=30s` — reclaims idle connections.
   - `DB_MAX_CONN_LIFETIME=1h` — rotates connections to pick up DNS/config changes.

2. **Statement timeout** — configured via `pgxpool` RuntimeParams. Prevents runaway queries from holding connections indefinitely.

3. **Advisory lock timeout** — `SET LOCAL lock_timeout = '5s'` ensures lock waits do not pin connections.

4. **SSE connection limits** — SSE streams should have a maximum lifetime and reconnect backoff.

5. **Alerting** — set up a Prometheus alert rule:
   ```yaml
   - alert: PostgresPoolNearExhaustion
     expr: loopzero_postgres_connections_active / 25 > 0.8
     for: 2m
     labels:
       severity: warning
     annotations:
       summary: "PG pool at {{ $value | humanizePercentage }} capacity"
   ```

## Related Alerts

| Alert | Condition |
|-------|-----------|
| `LoopZeroAPIDown` | API pod not responding to health checks |
| `LoopZeroDeployFailureRate` | Deploy failure rate > 20% in 15 min window |
| `PostgresPoolNearExhaustion` | Active connections > 80% of MaxConns for 2 min |
| `LoopZeroLockConflicts` | `loopzero_lock_conflicts_total` rate > 5/min |
