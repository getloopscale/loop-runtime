# Runbook: Soak Test Failure

> **Severity:** P1 (when CronJob fails) / P0 (when memory/FD alert fires)
> **Owner:** Platform team
> **Last updated:** 2026-05-05

## Overview

The weekly soak test (`k6-soak-weekly` CronJob) runs every Sunday 02:00 UTC for 6 hours at L2 load (1 deploy/min, 50 tenants). It validates the platform doesn't suffer from temporal degradation: memory leaks, file descriptor exhaustion, connection pool starvation, or GC pressure.

## Alerts

| Alert | Severity | Meaning |
|-------|----------|---------|
| `LoopZeroSoakTestFailed` | warning | k6 Job exited non-zero (threshold breach or crash) |
| `LoopZeroAPIMemoryLeak` | critical | `process_resident_memory_bytes` rising for 2+ hours during soak |
| `LoopZeroAPIFDExhaustion` | critical | `process_open_fds` rising for 2+ hours during soak |

## Triage Decision Tree

```
Alert fires
  │
  ├─ LoopZeroSoakTestFailed?
  │     └─ Go to Scenario 1
  │
  ├─ LoopZeroAPIMemoryLeak?
  │     └─ Go to Scenario 2
  │
  └─ LoopZeroAPIFDExhaustion?
        └─ Go to Scenario 3
```

---

## Scenario 1: Soak Test Job Failed

The k6 threshold was breached (e.g., >1% build failures, p99 too high).

### Diagnosis

```bash
# 1. Check Job status
kubectl -n loopzero-system get jobs -l app.kubernetes.io/name=k6-soak --sort-by=.metadata.creationTimestamp

# 2. Read k6 output (the report shows what threshold failed)
kubectl -n loopzero-system logs -l app.kubernetes.io/name=k6-soak -c k6 --tail=100

# 3. Check init container logs (seed or fixture failure?)
kubectl -n loopzero-system logs -l app.kubernetes.io/name=k6-soak -c seed-db
kubectl -n loopzero-system logs -l app.kubernetes.io/name=k6-soak -c build-fixture

# 4. Check API health during test window (Sunday 02:00-08:00 UTC)
# Grafana: loopzero_api_request_duration_seconds panel, filter last Sunday
```

### Common Causes

| Cause | Evidence | Fix |
|-------|----------|-----|
| DB seed failed | seed-db container log shows SQL error | Fix seed SQL or check DATABASE_URL secret |
| API was down during test | k6 log shows "health check failed" | Check API pod restarts during Sunday 02:00 UTC |
| Build plane saturated | k6 log shows >1% build failures | Check Argo Workflow queue depth, BuildKit capacity |
| Threshold too strict | Failures are borderline (e.g., 98.9% vs 99%) | Evaluate if threshold needs adjustment (ADR required) |
| Real regression | New code deployed Friday/Saturday degraded performance | Identify commit, rollback or fix |

### Resolution

1. If infra issue (API down, seed failure): fix root cause, re-run manually:
   ```bash
   kubectl -n loopzero-system create job k6-soak-manual --from=cronjob/k6-soak-weekly
   kubectl -n loopzero-system logs -f job/k6-soak-manual -c k6
   ```
2. If real regression: open P1 ticket, bisect to commit, fix before next Sunday.
3. If threshold too strict: requires ADR + plan amendment per governance rules.

---

## Scenario 2: Memory Leak Detected

`LoopZeroAPIMemoryLeak` fires when `deriv(process_resident_memory_bytes{job="loopzero-api"}[1h]) > 0` for 2+ consecutive hours during the soak window.

### Diagnosis

```bash
# 1. Confirm the leak is real (not just normal GC sawtooth)
# Grafana query: rate of RSS growth
# PromQL: deriv(process_resident_memory_bytes{job="loopzero-api"}[1h])

# 2. Check goroutine count (goroutine leak = memory leak)
# PromQL: go_goroutines{job="loopzero-api"}
# If growing linearly → goroutine leak

# 3. Get heap profile (if pprof enabled)
kubectl -n loopzero-system port-forward deploy/loopzero-api 6060:6060
curl -s http://localhost:6060/debug/pprof/heap > /tmp/heap.prof
go tool pprof -top /tmp/heap.prof

# 4. Check for connection pool leak
# PromQL: pgx_pool_total_conns{job="loopzero-api"}
# If growing beyond max_pool_size → connection leak

# 5. Compare with previous soak (Grafana annotations show historical)
```

### Common Causes

| Cause | Evidence | Fix |
|-------|----------|-----|
| Goroutine leak | `go_goroutines` growing linearly | Find ctx.Done() not being respected, channel not closed |
| Connection pool leak | `pgx_pool_total_conns` > max | Find `Acquire()` without matching `Release()` or deferred rollback |
| Response body not closed | RSS grows, no goroutine growth | Find `http.Get()` without `defer resp.Body.Close()` |
| Cache unbounded | RSS grows on specific endpoints | Find in-memory cache without eviction (add LRU or TTL) |
| SSE connection leak | goroutines + FDs growing together | Find SSE handler not cleaning up on client disconnect |

### Resolution

1. Get heap profile during next soak (or trigger manual soak)
2. Identify allocation source via pprof
3. Fix root cause (usually: missing defer, unbounded cache, or goroutine leak)
4. Validate fix: run `task test:load:soak` locally for 1h, confirm RSS stable

---

## Scenario 3: File Descriptor Exhaustion

`LoopZeroAPIFDExhaustion` fires when `deriv(process_open_fds{job="loopzero-api"}[1h]) > 0` for 2+ consecutive hours.

### Diagnosis

```bash
# 1. Current FD count vs limit
kubectl -n loopzero-system exec deploy/loopzero-api -- cat /proc/1/limits | grep "open files"
kubectl -n loopzero-system exec deploy/loopzero-api -- ls /proc/1/fd | wc -l

# 2. What's holding FDs open?
kubectl -n loopzero-system exec deploy/loopzero-api -- ls -la /proc/1/fd | tail -20
# Look for: socket (connections), pipe (goroutines), anon_inode (epoll)

# 3. Check if it correlates with connections
# PromQL: process_open_fds{job="loopzero-api"} vs pgx_pool_total_conns + http connections

# 4. Check file descriptors over time
# PromQL: process_open_fds{job="loopzero-api"}
# Normal: sawtooth or stable. Bad: monotonically increasing.
```

### Common Causes

| Cause | Evidence | Fix |
|-------|----------|-----|
| HTTP connection not closed | FDs are sockets, no pool saturation | Find HTTP client without connection reuse or timeout |
| File handles not closed | FDs are regular files | Find `os.Open()` without `defer f.Close()` |
| Pipe leak (exec.Command) | FDs are pipes | Find subprocess spawn without stdout/stderr close |
| Too many SSE connections | FDs are sockets growing with SSE clients | Add SSE connection limit or cleanup on timeout |

### Resolution

1. Identify FD type via `/proc/1/fd` inspection
2. Correlate with code path (HTTP client, DB pool, file ops, subprocess)
3. Fix root cause (usually: missing Close(), missing timeout, or connection reuse)
4. Set `ulimit` alert at 80% capacity as early warning

---

## Prevention

- **Every HTTP response body must be closed** — `defer resp.Body.Close()` immediately after error check
- **Every DB connection must be released** — `defer tx.Rollback()` or `defer conn.Release()`
- **Every goroutine must have an exit path** — `select { case <-ctx.Done(): return }`
- **Soak test runs weekly** — degradation caught within 7 days of introduction
- **Grafana dashboard** — `load-test.json` shows RSS, FDs, goroutines over time

## Related

- Dashboard: Grafana → LoopZero Platform → Load Test
- CronJob: `kubectl -n loopzero-system get cronjob k6-soak-weekly`
- Alerts: `kubectl -n loopzero-system get prometheusrule k6-soak-alerts -o yaml`
- Scripts: `infra/tests/k6/scripts/soak-6h.js`
- Plan: `docs/plans/load-testing-professional-plan.md`
