# Runbook: LoopZeroDBPoolSaturation

**Severity:** Warning
**Component:** loopzero-api (PostgreSQL connection pool)
**SLO impact:** API availability, deploy latency

## Alert expression

```
loopzero_pgxpool_acquired_conns{...} / loopzero_pgxpool_max_conns{...} > 0.8
```

Triggered when more than 80% of the PostgreSQL pool is in use for 5+ minutes.

## What this means

The API is approaching connection exhaustion. New requests will queue (waiting for `acquireTimeout`); long queries hold connections; risk of cascade if pool fully saturates → 503s and idempotency lock contention.

## Triage (first 5 min)

1. Check pool stats: `kubectl -n loopzero-system port-forward svc/loopzero-api 9090 &; curl localhost:9090/metrics | grep loopzero_pgxpool`
2. Check slow queries:
   ```
   kubectl -n loopzero-system exec -it <pg-pod> -- psql -c "SELECT pid, now() - query_start AS duration, state, query FROM pg_stat_activity WHERE state != 'idle' ORDER BY duration DESC LIMIT 20;"
   ```
3. Check API request rate: Grafana dashboard "API Latency" → look for spikes.

## Diagnose

- **Burst traffic** (deploy storm): rate of `loopzero_deploy_attempts_total` 10x baseline → throttle CLI clients.
- **Slow query**: queries > 1s → check `pg_stat_statements` top_calls.
- **Connection leak**: pool growing monotonically with no recovery → restart `loopzero-api` pods, file bug.
- **PG saturation**: PG `max_connections` reached upstream → scale DB or reduce pool max.

## Mitigate

| Cause | Action |
|-------|--------|
| Burst traffic | `kubectl -n loopzero-system scale deploy loopzero-api --replicas=N` (more replicas reduce per-pod pool pressure) |
| Slow query | Kill query via `pg_terminate_backend(pid)`; open hotfix |
| Connection leak | Rolling restart: `kubectl -n loopzero-system rollout restart deploy loopzero-api` |
| PG saturation | Coordinate with DB owner; scale managed DO PG vertically |

## Verify recovery

- Pool ratio < 0.5 sustained for 10 min
- API p99 latency back to baseline (< 500ms)
- No new alert firing

## Escalation

If pool stays > 80% after mitigation: page on-call platform engineer; consider cell migration or partial degradation mode.

## Related

- ADR: `.claude/decisions/...` (PG pool sizing)
- Metric: `loopzero_pgxpool_acquired_conns`, `loopzero_pgxpool_max_conns`
- Source: `api/internal/db/pgxpool.go`, `api/internal/middleware/pgcircuitbreaker/`
