# L3 Readiness Checklist (Scale 50k Plan)

**Target:** L3 — 100 tenants sustained, 10 deploys/min, 1 hour. Decision point on per-project ArgoCD Applications.
**Last updated:** 2026-04-17

## Infra sizing (per `scale-50k-plan.md` Phase 2.1)

| Requirement | Target | Current | Status |
|---|---|---|---|
| Runtime cluster maxNodes | 4 | **6** | ✅ (over-provisioned) |
| Mgmt cluster maxNodes | 4 | 3 | ❌ Pending |
| API replicas | 4 (HPA min=2 max=6 target 70%) | **HPA 2-6 active** | ✅ |
| ArgoCD controller memory | 1Gi | **1Gi** | ✅ |
| ArgoCD status-processors | 40 | 40 (env var set) | ✅ |
| ArgoCD operation-processors | 20 | 20 (env var set) | ✅ |
| Gitea memory | 1Gi | **1Gi** | ✅ |

**Remaining gap:** mgmt maxNodes 3 → 4. Blocked by Pulumi state drift (same reason as runtime 3→6 was via doctl). Can apply via doctl with same precedent if needed before L3.

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

## References

- [scale-50k-plan.md](scale-50k-plan.md) Phase 2.1
- [scale-50k-l2-learnings.md](scale-50k-l2-learnings.md) — L2 bugs + patterns
- Operator fix: commit a5601d1
- L3 capacity commits: see git log for ArgoCD/Gitea patches
