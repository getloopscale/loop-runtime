# Runbook: API Scaling Constraints

## Current Limitation

The LoopZero API runs **6 background goroutines** that are singleton by design:

1. `BuildReconcileLoop` — polls Argo Workflow CRDs, transitions build states
2. `BuildReaper` — enforces build timeout state machine transitions
3. `ReleaseReaper` — enforces release timeout state machine transitions
4. `ReleaseConvergenceLoop` — polls ArgoCD status, activates releases
5. `IdempotencyCleaner` — prunes expired idempotency keys
6. `SSEEventPruner` — removes old SSE events

These loops use process-local state (`sync.Map` for active tracking, semaphore for concurrency).
Running 2+ replicas would cause **duplicate processing** of the same builds/releases.

## Leader Election Guard (MEET-2026-03-31-023 T2-11)

The API uses `pg_try_advisory_lock` at startup to elect a leader:
- **Leader replica:** runs HTTP server + all 6 background loops
- **Non-leader replicas:** run HTTP server only (reads, SSE, auth work fine)

The lock is session-scoped (`pg_try_advisory_lock`, not `pg_try_advisory_xact_lock`):
- Held for the entire process lifetime
- Released automatically when the DB connection closes (process exit)
- New leader elected when old leader's connection drops

### Lock ID

`0x7468656F6C656164` ("loopzerolead" in hex) — constant in `api/cmd/main.go`.

## What Works Without Leader

| Feature | Works? | Notes |
|---------|--------|-------|
| HTTP API (reads) | Yes | All read endpoints work |
| HTTP API (mutations) | Yes | Lock, idempotency, build submission all work |
| SSE streaming | Yes | Event delivery from PostgreSQL LISTEN/NOTIFY |
| Auth (GitHub token validation) | Yes | Stateless |
| Build reconciliation | No | Only on leader |
| Release convergence | No | Only on leader |
| Build/release timeout enforcement | No | Only on leader |

## When to Scale

**Do NOT scale the API Deployment to 2+ replicas until reconcile loops are extracted.**

Extraction plan:
1. **Phase 1:** Move `BuildReconcileLoop` and `ReleaseConvergenceLoop` to a standalone binary
2. **Phase 2:** Move reapers to a CronJob or controller
3. **After extraction:** API Deployment can scale freely

## Troubleshooting

### Non-leader log message

```
WARN another API instance holds the leader lock — background loops will NOT start on this replica
```

This is expected if 2+ replicas are running. Only one will hold the leader lock.

### No leader (all replicas show the warning)

This should not happen. Check:
1. PostgreSQL connectivity from all replicas
2. The advisory lock: `SELECT * FROM pg_locks WHERE locktype = 'advisory' AND objid = 8241905828614915172`
3. If the lock is held by a dead connection, it will be released when PostgreSQL detects the disconnect (tcp_keepalive)
