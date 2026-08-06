# Edge Case Review — metering-compute-loop (v1.2)

Date: 2026-07-01
Tasks analyzed: 8 boundary-touching (0.1, 1.3, 2.1, 2.2, 2.3, 3.1, 3.2, 6.1)
Cases found: 11 (EDGE: 7, NEGATIVE: 4 | MUST FIX: 4, SHOULD TEST: 4, DOCUMENT: 3)

> Scope note: v1.2 already absorbed 21 conditions from MEET-2026-07-01-002 + a full failure-scenarios matrix (fence, CAS, clock-skew, period-rollover, RLS bypass+twin, crash mid-tx, mutable-label, foreign-ns, cell-unreachable, panic, breaker). This review targets what remains: the **allocation-metering model math** (Kubernetes pod-request edges) and a few NEGATIVE gaps in the new data sources (`plan_tier`, `metering_enabled`, the fencing subquery). None duplicates a covered case.

## MUST FIX

### EC-1: KEDA scale-to-zero — cursor must advance even when Σrequests = 0
- **Affected task:** T3.1 (collect + MeterCompute call), T2.3 (CTE)
- **Kind:** EDGE (extreme of valid: a tenant validly running 0 pods)
- **Family:** State / Timing
- **Scenario:** loopzero ships **KEDA scale-to-zero** (CLAUDE.md — `minReplicaCount=0`). A tenant scales to zero for hours, then scales up. If the worker skips `MeterCompute` when `collect` returns Σreq=0 (nothing to bill), `last_metered_at` never advances during the idle window. On scale-up, the next tick computes `elapsed = now − (hours ago)` × the **new** request → bills the entire idle window at the running rate (clamped by ADR-8 to `k×interval`, so it becomes a full-clamp spike every scale-up, and `loopzero_metering_clamped_total` fires on a *normal* event).
- **Impact:** systematic over-bill of every scale-to-zero → scale-up transition; clamp metric noise masks real outages.
- **Suggested fix:** the worker MUST call `MeterCompute` for every ACTIVE tenant every tick **even at Σreq=0** (the CTE advances the cursor and adds `0`); a zero-pod tenant bills 0 for the idle window, and scale-up bills only from `now`. Add to T3.1: "invoke MeterCompute per (tenant,metric,cell) unconditionally; Σreq=0 is a valid delta of 0."

### EC-2: Unscheduled / terminal pods counted as reserved compute
- **Affected task:** T3.1 (collect.go)
- **Kind:** NEGATIVE (invalid-to-bill state) + EDGE (boundary of "running")
- **Family:** State / Format
- **Scenario:** `collect` sums `pod.Spec.Requests` for **all** listed pods. A pod that is `Pending`/unschedulable (`status.Phase=Pending`, `spec.NodeName==""`), `Succeeded`/`Failed` (completed Jobs), or long-`Terminating` still carries `spec.Requests`. The reference (sealos `monitorPodResourceUsage:458`) explicitly skips `NodeName==""` and `Succeeded && age>1min`. The plan does not filter by phase.
- **Impact:** bills compute the tenant never actually reserved on a node (Pending that never schedules; completed jobs; zombie terminating pods) → over-bill.
- **Suggested fix:** in `collect`, count a pod only if `pod.Spec.NodeName != ""` AND `pod.Status.Phase ∈ {Running, Pending-but-scheduled}` and NOT (`Succeeded`/`Failed`). One `if` per pod; mirror sealos's filter.

### EC-3: Naive container sum over-bills init + sidecar containers
- **Affected task:** T3.1 (collect.go)
- **Kind:** EDGE (boundary of the pod-request formula)
- **Family:** Format
- **Scenario:** a pod's **effective** request is NOT `Σ all containers`. Kubernetes computes `max(Σ initContainers, Σ regularContainers + Σ native-sidecars)` — init containers run sequentially (their max, not added to regular), and native sidecars (restartPolicy=Always init containers, k8s ≥1.28) DO add. A naive `Σ pod.Spec.Containers[].Requests` (ignoring initContainers) under-bills a heavy-init pod, and a naive `Σ (containers + initContainers)` over-bills every pod with an init container (very common — migrations, asset builds).
- **Impact:** wrong bill on any pod with init/sidecar containers — the majority of real workloads.
- **Suggested fix:** T3.1 collect uses the k8s effective-request formula (`max(Σinit, Σregular + Σsidecar)`) — reuse `k8s.io/component-helpers/resource.PodRequests` if available, else implement the `max` explicitly. Add `TestCollect_InitAndSidecarContainerMath`.

### EC-4: `plan_tier` at meter time is unspecified (cost_counters CHECK requires it)
- **Affected task:** T2.3 (CTE insert), T3.1
- **Kind:** NEGATIVE (missing required value)
- **Family:** State / Permission
- **Scenario:** `cost_counters.plan_tier` is `NOT NULL CHECK (IN ('starter','pro','team','enterprise'))` (mig 000017). The ADR-4 CTE inserts `plan_tier=$plan` but the plan never says where `$plan` comes from at meter time — it must be read from the tenant's current subscription (`subscriptions` table; `cost.sql` already reads `plan_tier`). A tenant with no subscription row / an unrecognized plan → CHECK violation → the whole tenant's write aborts (and, per ADR-4, the cursor doesn't advance → re-meter next tick → permanent stall for that tenant).
- **Impact:** a tenant without a clean subscription row is silently never metered (write aborts every tick).
- **Suggested fix:** T3.1 reads the tenant's `plan_tier` from `subscriptions` during enumeration (join it into `ListActiveTenantsWithCell`); default to `'starter'` with a WARN + `loopzero_metering_missing_plan_total` when absent (never abort the write). Add `TestMeterCompute_UnknownPlanDefaultsStarter`.

## SHOULD TEST

### EC-5: `metering_enabled` DB row absent on fresh deploy → undefined kill-switch default
- **Affected task:** T3.2
- **Kind:** NEGATIVE (missing config)
- **Suggested test:** `test_metering_loop_disabled_when_flag_row_missing` — assert the per-tick kill-switch read defaults to a defined value when the `metering_enabled` row does not exist (recommend **disabled** until explicitly enabled, or seed the row `true` in mig 000045). NEGATIVE → assert the loop does not panic and picks the documented default, not a nil-deref.

### EC-6: Fencing subquery returns NULL when the metering loop has no `leader_generations` row yet
- **Affected task:** T2.3 (ADR-4 CTE), T3.2
- **Kind:** NEGATIVE (missing fencing authority)
- **Suggested test:** `test_meter_compute_no_generation_row_is_visible_not_silent` — if `(SELECT generation_id FROM leader_generations WHERE name=$loop AND cell_id=$leaderCell)` returns NULL (row not yet written for the metering loop's own lease name), `$gen = NULL` is NULL → 0 rows → **silent permanent no-op**. Assert the loop's generation authority is written on `Acquire` BEFORE the first meter write (SRE R4) AND that a missing row surfaces as a loud error / `loopzero_metering_fence_unresolved_total`, not silent 0-rows. NEGATIVE → assert the typed error / metric, not silent skip.

### EC-7: Period-boundary function must be deterministic UTC
- **Affected task:** T2.3 (ADR-9 split), T6.1
- **Kind:** EDGE (boundary of the billing period)
- **Suggested test:** `test_period_boundary_is_utc_deterministic` — assert `period_start`/`period_end` are computed in **UTC** from a pure function (not server-local time / no DST). A tick at 23:30 local near a month boundary must not misclassify the period. EDGE → assert exact boundary timestamps at month-end in UTC.

### EC-8: Limits-only pod (requests empty, limits set)
- **Affected task:** T3.1 (collect.go)
- **Kind:** EDGE (valid pod with only limits)
- **Suggested test:** `test_collect_limits_only_pod` — a container with `limits` but no `requests`: Kubernetes defaults requests to limits at admission, but raw `pod.Spec.Containers[].Resources.Requests` may be empty. Decide + assert: fall back to limits when requests is empty (matches k8s effective behavior) so a limits-only pod is not billed 0. EDGE → assert the limit value is used at the boundary.

## DOCUMENT

### EC-9: Very large pod count per namespace → raw List size
- **Kind:** EDGE
- **Accepted risk:** ADR-11 already commits to raw List now + informer migration gated on `loopzero_metering_pass_duration_seconds`. Add a one-line note that the List uses `limit`/`continue` paging so a namespace with thousands of pods does not exceed the apiserver response budget. Bounded; observe-only.

### EC-10: Namespace/project/env created mid-tick
- **Kind:** EDGE
- **Accepted risk:** enumeration snapshots at tick start; a namespace created during the pass is metered on the next tick (delay ≤ one interval). Acceptable — bounded, and the cursor bootstraps to `now()` (0 charge) on first sight.

### EC-11: Dedicated worker pool (2-4 conns) stall under lock contention
- **Kind:** NEGATIVE
- **Accepted risk:** ADR-10's dedicated pool isolates the blast radius from the API pool (the whole point). A stalled meter write starves only the metering pass, surfaced by `loopzero_metering_seconds_since_last_success` + `MeteringStalled`. Give the worker's writes a context timeout; document that pool exhaustion self-heals next tick.

## Summary

| Task | EDGE | NEGATIVE | MUST FIX | SHOULD TEST | DOCUMENT |
|------|------|----------|----------|-------------|----------|
| T2.3 | 1 (EC-7) | 2 (EC-4,EC-6) | 1 (EC-4) | 2 (EC-6,EC-7) | 0 |
| T3.1 | 3 (EC-1,EC-3,EC-8) | 1 (EC-2) | 3 (EC-1,EC-2,EC-3) | 1 (EC-8) | 1 (EC-10) |
| T3.2 | 0 | 1 (EC-5) | 0 | 1 (EC-5) | 1 (EC-11) |
| T6.1 | 1 (EC-7 shared) | 0 | 0 | 0 | 1 (EC-9) |

**Coverage check:** every boundary-touching task considered BOTH lenses. T2.1/T2.2 (migrations) + T0.1/T1.x (contracts/guards) are pure-schema/pure-data — their edges (dirty replay, BIGINT overflow, drift) are already covered in the plan; no new lens applies.

**Verdict:** PLAN NEEDS ADJUSTMENT — absorb EC-1..EC-4 (MUST FIX, all allocation-model correctness bugs producing wrong bills) into v1.3 as sub-tasks of T3.1/T2.3; add EC-5..EC-8 as TDD tests to their tasks; EC-9..EC-11 as plan notes. None requires architectural rework — each is an `if` / a formula / a data-source line / a test.
