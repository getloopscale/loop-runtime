---
type: finding
title: non-atomic status mutation via nil-tx pool fallback (pre-existing)
---

# FINDING — non-atomic status mutation via nil-tx pool fallback (pre-existing)

- **Date:** 2026-07-03
- **Discovered during:** M9 Step 5 db-split design review ([`docs/meetings/records/MEET-2026-07-03-001.md`](../../meetings/records/MEET-2026-07-03-001.md)), distributed-systems-engineer ratification.
- **Severity:** HIGH (state-machine status mutation without atomicity or advisory-lock scope).
- **Filed to GitHub:** NOT filed — `gh` unauthenticated for `getloopscale/loopzero` in this environment. This doc is the durable record.
- **Introduced by:** NOT M9. Pre-existing; surfaced by the design review.

## Summary (expected vs actual)

The `<Entity>StoreSqlc.UpdateStatus` status-mutation path is a read-modify-write: `GetByIDTx(tx) → SafeTransition/CAS → generated.Update*Status(tx)`. The sqlc bridge `gen(tx)` falls back to the connection **pool** when `tx == nil` (`internal/db/release_wrapper.go:147-160`). When a caller passes `nil`, the fetch and the UPDATE run as **two separate auto-committed statements on two different pooled connections** — so:

- **Expected:** fetch + validate + update execute atomically inside one transaction holding `pg_advisory_xact_lock` (the `withEnvironmentLock` contract).
- **Actual (nil-tx callers):** no atomicity between fetch and update; the advisory lock is never held for the mutation; a concurrent mutation can interleave between the read and the write.

## Affected call sites (measured)

```
internal/services/preview_service.go:380
    s.deps.Store.UpdateStatus(ctx, nil, previewID, newStatus)
internal/services/build_orchestrator_submit.go:213
    o.buildStore.UpdateStatus(ctx, nil, buildID, string(statemachine.BPEventStarted))
internal/services/build_orchestrator_submit.go:326
    o.buildStore.UpdateStatus(ctx, nil, buildID, string(statemachine.BPEventFailed))
```

The other 25 status-mutation call sites correctly pass a real `tx` (grep: `.UpdateStatus(ctx, tx,` — 25 tx vs 3 nil).

## Impact

- `build` status transitions on submit-start / submit-fail and `preview` status updates are non-atomic. For build/release the state machine still validates the transition, but the read-then-write can race a concurrent transition (lost update / double-apply window). For a machine-enforced entity the second write would re-validate, but the advisory-lock guarantee that services rely on is silently absent on these paths.
- This is the exact hazard the M9 Step 5 db-split must NOT widen: if the split ever fragments the read-modify-write across two calls while a caller passes nil, the two halves land on different pooled connections.

## Recommended corrective action (pre-condition 2 of M9 slice 5b)

1. Make `UpdateStatus` (all 4 status-bearing stores) **reject `nil` tx** with a typed error — a status read-modify-write must never fall back to the pool.
2. Fix the 3 callers to pass a real `tx` (wrap the mutation in a transaction via the existing tx-begin helper, inside `withEnvironmentLock` where applicable).
3. Add a regression test per caller asserting the mutation runs in a tx (and a unit test asserting `UpdateStatus(ctx, nil, …)` returns the typed nil-tx error).
4. Sequence this BEFORE the 5b store-layer move (per MEET-2026-07-03-001), so the split lands on an already-atomic path.

This is a T2 correctness change (concurrency semantics) with its own analysis — do NOT bundle it into the behavior-preserving relocation.
