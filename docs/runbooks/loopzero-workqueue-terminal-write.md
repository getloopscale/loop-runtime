# Runbook: LoopZeroWorkqueueTerminalWriteFailed

## Alert

`loopzero_workqueue_terminal_write_fail_total{terminal=complete|fail}` rate > 0 for 2m.

## Severity

Warning. **Risk class differs by `terminal` label:**

- **`terminal=complete`** — DUPLICATE-SIDE-EFFECTS class. The handler succeeded but the DB Complete write failed. The item stays InFlight and will be re-dequeued; the handler runs AGAIN. For build/release reconcile this means duplicate Argo Workflow submissions or duplicate notifications.
- **`terminal=fail`** — ORPHAN-ROW class. The handler failed and the DB Fail write also failed. The InFlight row never reaches a terminal state; orphan rows accumulate in `workqueue` table.

## Quick check

```bash
kubectl --context loopzero-mgmt -n loopzero-system logs deploy/loopzero-api --tail=200 | \
  grep -E "queue\.(Fail|Complete) terminal write failed"
```

```sql
-- Stuck InFlight rows
SELECT queue, status, COUNT(*), max(updated_at) AS oldest
FROM workqueue
WHERE status = 'inflight'
GROUP BY queue, status
ORDER BY oldest;
```

Rows older than ~5 minutes in `inflight` state are stuck.

## Root cause classes

1. PG connection pool exhausted (concurrent terminal writes timing out).
2. Network blip between API and PG (transient — should self-resolve on retry).
3. Schema drift — `workqueue` table CHECK constraint rejects new state value.

## Recovery

For terminal=complete (DUPLICATE risk):
1. Check downstream side-effect for duplicates (Argo Workflows for the affected item_id).
2. Manually mark the workqueue row as `complete`: `UPDATE workqueue SET status='complete' WHERE id='<item_id>'`.

For terminal=fail (ORPHAN risk):
1. Run cleanup: `UPDATE workqueue SET status='failed', error='manual cleanup post-DB-write-fail' WHERE status='inflight' AND updated_at < now() - interval '10 minutes'`.

## References

- find_workqueue_silent_db_errors (deep-review-remediation T1.7).
- CLAUDE.md Rule 8 — never engole exceções.
