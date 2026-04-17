# L3 Readiness Checklist (Scale 50k Plan)

**Target:** L3 — 100 tenants sustained, 10 deploys/min, 1 hour. Decision point on per-project ArgoCD Applications.
**Last updated:** 2026-04-17

## L3 v8: PARTIAL FAIL ⚠️ (2026-04-17 14:32 UTC — best yet)

**Result:** 75/100 succeeded, 25 failed. Still below 99.5% gate, mas **maior sucesso histórico**.

Setup dessa rodada (após plano T-01..T-07 de `50k-immediate-focus.md`):
- 8 HPAs ativos (Gitea, Harbor core/registry/jobservice, ArgoCD server/repo-server, Ory hydra/kratos, mgmt-traefik)
- ArgoCD application-controller replicas=3 + sharding
- Argo quota limits 16→48 CPU / 32→96Gi memory
- cosign HOME=/tmp + TUF_ROOT (bug #39 fixed)
- Workspace PVC = local-path (bug #21/#40 fixed — bypass DO CSI rate limit)
- RBAC pods+namespaces:delete (bugs #36/#37 fixed)
- Concurrency 30 + CLI/harness retry 429/503 (bug #43 admission wiring done)

Breakdown dos 25 failures:
- **18 upload failures** (Gitea 401 HTTP 500) — burst de 30 pushes antes HPA Gitea escalar 2→5 (~60s window). Fix: `minReplicas: 4` commited para próxima rodada.
- **4 build failures** — `Max duration limit exceeded` em `build-image-api` (BuildKit retry MaxDuration 60s insuficiente sob CPU contenção).
- **3 verify failures** (tn-78/79/96) — pods subiram mas HTTP 200 não retornou em 240s (late scheduling ou ArgoCD sync atrasado).

Evolução histórica L3:
| Versão | Success | Data | Causa principal |
|---|---|---|---|
| v1 | 66/100 | 10:25 UTC | bugs #18/#35/#36 cascading |
| v2 | ~40/100 | 11:00 UTC | cosign TUF (bug #39) + quota (bug #41) |
| v3 | killed early | 12:00 UTC | admission wiring discovered |
| v4 | ~40/100 | 12:20 UTC | DO CSI 429 at c=20 |
| v5 | ~40/100 | 12:40 UTC | orphan ArgoCD apps from force-null finalizers |
| v6 | ~30/100 | 13:00 UTC | dirty state from prior runs |
| v7 | ~30/100 | 13:15 UTC | SOURCE_UPLOAD_FAILED Gitea saturation |
| **v8** | **75/100** | 14:32 UTC | Gitea burst + BuildKit timeout |

**Próximas ações (L3 v9):**
1. Gitea `minReplicas: 4` já commitado (982dbbd) — ativo para próxima rodada
2. Investigar BuildKit retryStrategy MaxDuration (gap novo a abrir)
3. verifyAppLive 240s suficiente para 75/100; ArgoCD sync sharding ajudará

**Gate ainda não atingido.** Próxima iteração foco: 90-95/100.

---

## L3 v1: PARTIAL FAIL ⚠️ (2026-04-17 10:25 UTC — history)

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

## L3 v8 — Gitea HPA calibration (2026-04-17 14:39)

**Config:** c=30, Gitea HPA min=2 max=8 (pre-bug-#44-fix HA config).

**Result:** FAIL — 75/100 apps live. Best-ever to date.

| Metric | Value |
|---|---|
| Apps live | 75/100 (75%) |
| Upload failures | 18 (SOURCE_UPLOAD_FAILED HTTP 500) |
| Build failures | 4 (BuildKit MaxDuration exceeded) |
| Late failures | 3 (verifyAppLive timeout) |

**HPA behavior observed:** Gitea scaled 2→5 replicas, CPU peaked 206%
before stabilizing at 29%. Scaled correctly but reactive lag during
initial burst (18 uploads failed in first 60s).

**Root cause:** reactive HPA cannot absorb burst faster than ~60s
from scale decision to pod-ready. The 30-concurrent-push burst arrives
in <10s.

**Decision:** Gitea `minReplicas: 2 → 4` (baseline replicas absorb
initial burst while HPA ramps for sustained load). Also bumped BuildKit
`retryStrategy.MaxDuration` 60s → 900s. Plan: L3 v9.

---

## L3 v9-v13 — iterating with clean slate + API fixes (2026-04-17 afternoon)

Multiple iterations to close secondary bugs surfaced by v8:

| Version | Apps Live | wf_ok | Primary bug |
|---|---|---|---|
| v9  | 32/100 | 65  | admission gate env wiring (bug #43) |
| v10 | — | — | API deploy mid-test sabotage (env not wired; bug #43 round 2) |
| v11 | ~50/100 | — | dirty state (prior run tenants not evicted) |
| v12 | ~55/100 | — | Gitea HA config incomplete — session/cache in-memory per pod → 401s mid-stream (bug #44) |
| v13 | ~58/100 | 90 | Argo quota 403 + Gitea burst + runtime memory pressure |

**Bug #44 — Gitea HA multi-pod session divergence** discovered during
v12 analysis. When HPA scales beyond 1 replica with `provider: memory`
for session/cache/queue, auth state diverges per pod. Some requests
hit a pod with valid session, some hit a stale pod → intermittent 401
in the middle of a git push sequence.

**Fix (bfbe818):** session/cache/queue all backed by Redis (separate DB
per concern: 0/1/2). Plus PG pool config (MAX_OPEN_CONNS=8,
MAX_IDLE_CONNS=4, CONN_MAX_LIFETIME=3600s), indexer switched from
Bleve (incompatible with HA) to DB-backed for issues + disabled for repo.

---

## L3 v14 — post-HA-fix + Argo quota bumped (2026-04-17 16:53)

**Config:** c=30, Gitea HPA min=4 max=8, Argo quota 128 CPU / 256Gi / 200 pods,
API phase-1 (handlers wired, K8s teardown still via bash).

**Result:** FAIL — 62/100 apps live.

| Metric | Value |
|---|---|
| Builds succeeded | 97/101 (96%) ✅ above the 95% build target |
| Builds error | 4/101 (4%) |
| Apps live | 62/100 (62%) 🔴 below 95% app-live target |
| Apps Pending indefinite | 32/100 (32%) |
| Apps No Pod | 6/100 (6%) |

**Root cause NEW:** runtime cluster out of capacity. 32 apps stuck
Pending with Kubernetes event
`0/9 nodes are available: 3 Insufficient cpu, 6 Insufficient memory`.

**Cluster shape at test time:** 9 nodes — 3× `s-2vcpu-4gb` (default
Pulumi pool) + 6× overflow pools (`overflow-s-2vcpu-2gb`, `overflow2`)
created via doctl. Total effective memory after system pods ≈ 18Gi;
100 apps × 128Mi memory requests = 12.8Gi. Math fits on paper, but
with fragmentation + existing platform pod overhead, scheduling tops
out around 65-75 apps.

**What this proved:**
- HPAs (Gitea, Harbor, ArgoCD, Ory) all scaled correctly ✅
- Argo quota (128 CPU / 256Gi / 200 pods) absorbed the burst ✅
- BuildKit retry + cosign TUF_ROOT + admission gate env fix all
  working ✅ (96% build success rate confirms)
- Gitea HA config preserves auth state across replicas ✅ (0 401s in
  the sample)
- **Runtime scheduling capacity is now THE gap.** Software bugs that
  v1-v13 fought are all closed. What remains is infra sizing.

**Decision:** Add T-08 (Runtime capacity planning — runtime default pool
from s-2vcpu-4gb×3 to s-4vcpu-8gb×6) to the plan. Blocker for T-08 is
external: DO droplet quota increase (Paulo opens ticket).

**Other action items from SRE Guardian iter 15 — all closed:**
- Gitea HPA maxReplicas 8 → 12 (source ba26abb + live kubectl patch)
- 4 Error workflows (cleanup eviction removed them)
- Harbor canary broken (6ffa736 — harbor-admin-credentials secret + loopzero-platform project)

---

## L3 consolidated progress

| Version | Build Success | Apps Live | Dominant bottleneck |
|---|---|---|---|
| v1 | 66% | — | bugs #18/#35/#36 cascading |
| v2 | ~40% | — | cosign TUF + Argo quota |
| v3-v7 | 0-50% | — | dirty state + admission gate env |
| v8 | 75% | 75% | Gitea burst (pre-HPA minReplicas=4) + BuildKit timeout |
| v9-v13 | 40-90% | 32-58% | Gitea HA + Argo quota + runtime memory |
| **v14** | **96%** | **62%** | Runtime cluster scheduling capacity |

**Trajectory:** build pipeline success moved 66% → 96% through
software fixes. App-live success is now gated on infra sizing (T-08).
Each dominant bottleneck has shifted from "bug" toward "capacity
plan", which is the desired progression.

---

## References

- [scale-50k-plan.md](scale-50k-plan.md) Phase 2.1
- [scale-50k-l2-learnings.md](scale-50k-l2-learnings.md) — L2 bugs + patterns
- [50k-immediate-focus.md](50k-immediate-focus.md) — current plan (T-01..T-09, Gates)
- [capacity-baselines.md](capacity-baselines.md) — bench measurements (loopzero-api, minio, argocd, gitea)
- Operator fix: commit a5601d1
- L3 capacity commits: see git log for ArgoCD/Gitea patches
- Key commits this cycle: ba26abb (L3 v14 results + T-08), 94e8ae9 (API T-02 wired),
  e79b50e (DB teardown), ea04ba9 (K8s teardown), 4a9a077 (CLI wired), 6ffa736 (Harbor canary fix),
  bfbe818 (Gitea HA), 750024e (Gitea baseline)
