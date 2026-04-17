# L3 Readiness Checklist (Scale 50k Plan)

**Target:** L3 — 100 tenants sustained, 10 deploys/min, 1 hour. Decision point on per-project ArgoCD Applications.
**Last updated:** 2026-04-17

## L3 v1: PARTIAL FAIL ⚠️ (2026-04-17 10:25 UTC)

**Result:** 66/100 workflows Succeeded, 1 Failed (tn-34 cache timeout), 10 stuck Running.
L3 **did NOT meet gate** (requires 100/100).

**Root causes discovered (3 cascading bugs):**

1. **Bug #18 — DO CSI PVC saturation** (primary blocker). 100 tenant builds × 5Gi PVC =
   500Gi requested through DO CSI, which provisions serially (~3 PVCs/min). Workflows after
   ~66 hit the queue; pods stuck `Init:0/1` waiting for volume mounts. Already documented in
   `scale-50k-technical-debt.md` — L3 is the breaking scale.

2. **Bug #35 — MinIO single-pod saturation**. Mirror-harbor-api step hits MinIO for cache
   restore + image mirror. 100 concurrent builds overwhelm single standalone MinIO pod.
   8 `Error` pods observed in mirror-harbor step. Temporary live-fix applied: bumped
   resources to 2CPU/4Gi + MINIO_API_REQUESTS_MAX=10000.

3. **Bug #36 — RBAC gap for `kickStuckPullBackoffPods`** (live-fixed, not in v1 run).
   `loopzero-ctrl-runtime-sa` ClusterRole lacked `pods:delete` verb. Operator was logging
   403 on kick attempts, pods waited kubelet natural backoff (~5min) instead of recovering
   immediately. Fixed in commit ef93a1b + live patched on runtime cluster.

**Single-bug impact tolerable; 3 bugs cascading → system deadlock.**

L3 v2 plan (before re-run):
- Fix Bug #18: scale MinIO to distributed mode OR reduce per-workflow PVC from 5Gi→2Gi
- Fix Bug #35: MinIO distributed mode (4 nodes) with replicated pools
- Verify Bug #36 fix persistent after next Pulumi `up`

## L2 Gate: PASSED ✅ (v8, 2026-04-17 03:53 UTC)

```
REAL DEPLOY L2 — 50 TENANTS — RESULTS
  Succeeded:     50 / 50
  Apps Live:     50 / 50 (HTTP 200 verified)
  Failed:        0
--- PASS: TestRealDeploy_L2_50Tenants (2933.57s)
```

**What made v8 pass (after v4 47/50, v5 47/50, v6 45/50, v7 40/50):**
1. Operator fix — `ensurePullSecret` provisions pull secret atomically, `kickStuckPullBackoffPods`
   handles residual ImagePullBackOff race (commits a5601d1 + ca25f1a).
2. Harness fix — `verifyAppLive` deadline 120s → 240s covers autoscaler latency (commit 8c889dc).
3. Cluster pre-warm — 6 Ready nodes before deploy wave (3 default + 1 overflow s-2vcpu-2gb +
   2 overflow2 s-2vcpu-2gb). Avoids reactive autoscaling during deploy.

**Critical operational lesson:** Pre-warming cells is not optional at scale. v7 (40/50)
validated the loopzerory — 20 succeeded before DO SFO3 capacity crunch, 20 succeeded after overflow
pools came up, 10 failed during the 15-minute capacity-exhausted window. A warm cell passes;
a cold cell stragglers.

## Infra sizing (per `scale-50k-plan.md` Phase 2.1)

| Requirement | Target | Current | Status |
|---|---|---|---|
| Runtime cluster maxNodes | 4 (L3 plan), **10+** recommended | **6** (default) + overflow pools available | ✅ |
| Runtime pre-warmed nodes | ≥ expected peak | **6 Ready** (3 default + 1 overflow + 2 overflow2) | ✅ |
| Mgmt cluster maxNodes | 4 | 3 | ❌ Pending |
| API replicas | 4 (HPA min=2 max=6 target 70%) | **HPA 2-6 active** | ✅ |
| ArgoCD controller memory | 1Gi | **1Gi** | ✅ |
| ArgoCD status-processors | 40 | 40 (env var set) | ✅ |
| ArgoCD operation-processors | 20 | 20 (env var set) | ✅ |
| Gitea memory | 1Gi | **1Gi** | ✅ |

**Remaining gap:** mgmt maxNodes 3 → 4. Blocked by Pulumi state drift (same reason as runtime 3→6 was via doctl). Can apply via doctl with same precedent if needed before L3.

**For L3 (100 tenants), recommended pre-warm:** bump runtime to **8-10 nodes** before test start.
The L2 v8 run demonstrated that 6 nodes handle 50 tenants comfortably; extrapolating linearly,
L3's 100 tenants need ≥ 10 nodes. Script template: see `/tmp/bump-mgmt-nodes.sh` (adapt for
runtime pool ID 89569c9c-44e8-490b-a4cf-651f0101567a).

## Bug fixes required for L3

| Bug | Status | Notes |
|---|---|---|
| #24 imagePullSecret race | ✅ **Fixed** (commit a5601d1 + ca25f1a) | EnvironmentController provisions secret atomically + kicks stuck ImagePullBackOff pods |
| #25 autoscaler scale-up latency | ⚠️ **New finding** (L2 v6) | Pods start on new nodes but test times out during scale-up (~2min). See below. |
| Pulumi state drift | ❌ Open (T3) | Not blocking L3 if we avoid `pulumi up` |
| TLS rate limit (Origin CA migration) | ⏳ Pending Paulo key provisioning | Doesn't block L3 but resolves noise |

## Bug #25 — Cluster-autoscaler scale-up latency (discovered 2026-04-17, L2 v6)

**Symptom:** 2-3 tenants consistently fail per L2 run with `success=false` at ~6min elapsed, regardless
of bug #24 fix status. Same pattern across v4/v5/v6 (different tenant indices each run).

**Root cause:** Runtime cluster starts with 1 node (cost-optimization). Deploys in a wave trigger
cluster-autoscaler to add nodes. DO node provisioning takes ~90-120s. The first pod scheduled on
each new node waits for provisioning + image pull, total ~5-6min. Test timeout is typically 5min
per deploy wait.

Evidence (tn-20 in v6, namespace `loopzero-tn-lt-l2-tn-20-production`):
```
9m33s  Pod created
9m16s  TriggeredScaleUp: nodes 2→3 (max: 6)
7m38s  Pulling image (1m38s after scale-up — waiting for node)
7m32s  Pod Running (Ready 1/1)
6m17s  ← test reported failure (but pod was actually up ~1m later)
```

**Why kickStuckPullBackoffPods didn't help:** the issue is BEFORE the pod schedules on any node.
Pod is `Pending` with `Unschedulable` until node provisioned — not `ImagePullBackOff`.

**Mitigation options for L3:**

1. **Pre-warm runtime nodes to target capacity** (recommended) — before L3 starts, manually scale
   node pool to ~6 nodes. `doctl k8s cluster node-pool update <pool> --count 6`. At 50k scale,
   pre-warming is mandatory anyway; reactive autoscaling can't keep up with deploy rate.
2. **Increase test deploy timeout** (bandaid) — raise per-deploy timeout from 5min to 8min. Doesn't
   solve real-world problem but unblocks test.
3. **Bump cluster min nodes** (production-relevant) — set runtime pool `minNodes=3` instead of 1.
   Trades cost for availability.

**Impact on 50k plan:**
- At 50k apps across ~100 cells, each cell runs 500 apps on pre-warmed node pool.
- Reactive autoscaling during a deploy wave is not viable — deploys would straggle for 2min each.
- **L3 gate: validate pre-warm strategy before advancing.**

## Bug #26 — DO region capacity limits (discovered 2026-04-17, L2 v7)

**Symptom:** `doctl kubernetes cluster node-pool update` returns 422
`region has insufficient capacity to resize pool` for s-2vcpu-4gb in SFO3 during
an active L2 run. Autoscaler had scaled down from 5→2 nodes between tests; when
v7 deploys hit CPU pressure and triggered scale-up, DO rejected the request.

**Root cause:** DO regions have spot capacity for droplet sizes. Mid-test if
capacity is exhausted (by other DO customers), autoscaling stalls. This is
outside LoopZero's control.

**Implications for 50k plan:**

1. **Single-region dependency is a blast radius** — if a cell region runs out of
   capacity, all new deploys in that cell queue or fail. 100 cells in 1-2 regions
   compounds this risk.
2. **Reserved capacity** — for production, need committed capacity agreements with
   cloud providers (DO Spaces + Droplet reservations, AWS Savings Plans, etc.).
3. **Multi-region cell distribution** — cells should span 3+ regions so single-
   region capacity loss degrades to ~30% vs 100%.
4. **Graceful degradation** — when region capacity is exhausted, the placement
   engine should route new tenants to other cells/regions rather than fail.

**Mitigation for local dev:**
- Use smaller instance sizes (s-1vcpu-2gb) for test clusters — higher availability.
- Pre-warm with `minNodes >= expected peak` before running load tests (trades $ for predictability).
- Accept that DO SFO3 at the specific test run had no capacity — retry later or use NYC3/FRA1.

**Production-relevant lesson:** 50k apps across 100 cells cannot rely on reactive
autoscaling. Must combine: (a) pre-warmed cells at capacity, (b) capacity
reservations with provider, (c) cross-region distribution, (d) queue-and-retry
for overflow.

## L3 Scenarios (from plan Phase 2.1)

- [ ] **3.1 Sustained load** — 100 tenants, 10 deploys/min, 1 hour
- [ ] **3.2 Noisy neighbor** — 1 tenant doing 20 rapid deploys
- [ ] **3.3 ArgoCD pressure** — 100 apps, 50 syncing
- [ ] **3.4 Gitea pressure** — 200 repos, concurrent git push
- [ ] **3.5 SSE saturation** — 100 concurrent `loopzero deploy --watch`

## Chaos scenarios

- [ ] All 6 existing chaos scenarios under load (from plan Phase 1)
- [ ] ArgoCD OOM kill → restart + resync
- [ ] Gitea disk full → reject push, recover after cleanup
- [ ] SSE connection storm (200 clients connecting/disconnecting rapidly)

## Decision point

Per scale-50k-plan: **"Per-project ArgoCD Applications? If sync time > 5min → split."**

Criteria to decide during L3 3.3:
- Measure avg sync time with 100 apps on single Application
- If > 5min → implement per-project Applications (big architectural change)
- If < 5min → keep single Application per env, revisit at L4

## DoD

- [ ] 1 hour sustained load, stable metrics
- [ ] Bottleneck identified and documented
- [ ] 95/100 tenants live (SLO L3)
- [ ] No workflow failures (pipeline clean)
- [ ] Decision recorded on per-project Apps

## Rollback plan

If L3 causes severe issues:
- Mgmt memory > 90% → halt test, analyze, resize
- DB pool > 20/25 → circuit breaker will trip (designed behavior)
- API replicas HPA to 6 then OOM → bump limits to 512Mi

Each case: cancel via `pkill -9 -f real_deploy_test`, cleanup workflows, investigate before retry.

## Parallelism analysis (4 layers observed at L3 run)

| Layer | Concurrency | Notes |
|-------|-------------|-------|
| Harness client | 10 (L3), 5 (L2), 20 (L4), 50 (L5) | Semaphore in `real_deploy_test.go` |
| API admission gate | 20 builds/cell, 3/tenant | `MAX_CONCURRENT_BUILDS_PER_CELL` config |
| Argo Workflows | Capped by harness (10 at L3) | No saturation; admission allows 20 |
| Build DAG intra-workflow | Serial | clone→cache→install→build→sign (natural deps) |

**Observed bottleneck for L3:** harness concurrency is 10, not the admission gate (20) — room
to grow when saturating build cluster.

**Bug candidate #34 — serial bootstrap phase:** `ProvisionTenant` is called in a sequential
loop before deploy phase. At ~6s/tenant × 100 tenants = ~10min of pure wait. Parallelizing
with concurrency 5-10 would cut bootstrap to ~1-2min. Low priority (not on critical path for
L2/L3 gates), but consider for L4/L5 where bootstrap cost grows.

## References

- [scale-50k-plan.md](scale-50k-plan.md) Phase 2.1
- [scale-50k-l2-learnings.md](scale-50k-l2-learnings.md) — L2 bugs + patterns
- Operator fix: commit a5601d1
- L3 capacity commits: see git log for ArgoCD/Gitea patches
