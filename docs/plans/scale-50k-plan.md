# Scale Plan: 50,000 Fullstack Applications + Chaos Validation

**Date:** 2026-04-16
**Status:** Approved
**Owner:** Platform Engineering
**Prerequisite:** All building blocks implemented (leader fencing, PG claims, work queue, admission gate, circuit breaker, cell provisioning, dynamic router)
**Goal:** Close the gap between "code exists" and "production-validated at scale"

---

## Current State (Honest Assessment: 3.5 / 5)

The platform has all scale components **implemented and tested in isolation**, but most are **not wired as default in production**:

| Component | Code | Tests | Wired in prod? |
|-----------|:----:|:-----:|:--------------:|
| Controller binary (loop extraction) | Done | Done | No — behind `LOOPZERO_DISABLE_LEGACY_LOOPS` flag |
| Leader-generation fencing | Done | 18 tests | Only in controller binary |
| PG-backed processing claims | Done | 14 tests + 2 stress | No — loops still use `sync.Map` |
| Distributed work queue | Done | Done | No — loops don't enqueue |
| Build admission gate | Done | 4 tests | Wired but `LOOPZERO_ADMISSION_CONTROL=false` |
| PG circuit breaker | Done | Done | Wired and active |
| Rate limiter | Done | Done | Wired and active |
| SSE backpressure | Done | Done | Wired and active |
| DynamicRouter | Done | Done | No — `PassthroughRouter` used |
| K8sCellStore | Done | Done | No — not initialized |
| PlacementEngine (RoundRobin) | Done | Done | No — `DefaultEngine` used |
| CrossCellAggregator | Done | Done | No — not called |
| cell.ts (Pulumi provisioner) | Done | Done | No — not called from index.ts |
| Chaos tests | 6 scenarios | Done | Require manual flag |
| Load tests L0-L4 | Done | Runnable | Require manual flag |
| Load tests L5+ multi-cell | Scaffold | `t.Skip()` | Blocked on cell wiring |

**The gap is wiring and validation, not implementation.** Estimated complexity: 2/5.

---

## Target

**50,000 fullstack applications** distributed across multiple cells, validated with progressive load tests and chaos injection at every level.

**50,000 fullstack apps** = ~50,000 tenants × 1 project × 1 environment × 2 app units (server + static frontend) = **~100,000 pods + 50,000 ArgoCD apps + 100,000 Gitea repos**.

At 500 tenants/cell (validated at L4): **~100 cells**.

---

## Load Test Strategy: Real Dogfood, Not Synthetic Stubs

### Problem with current approach

The existing load test infrastructure has two modes, both inadequate for real validation:

1. **`seed_test.go`** — Inserts data directly into PostgreSQL (users, tenants, projects). This bypasses the entire platform: no Tenant CRs created, no Kubernetes namespaces provisioned, no Gitea repos, no ArgoCD Applications. Tenants exist in the database but **not in the infrastructure**. A deploy to these tenants would fail.

2. **`cell_scale_test.go`** — Calls the API (`POST /api/v1/projects`, upload, `POST /api/v1/builds`) but with **fake git bundles** (a minimal header that passes upload validation but causes build failure). Measures upload + queue latency only. **No build completes. No image pushed. No pod runs. No URL works.**

Neither approach tests what a real user does: `loopzero login` → `loopzero init` → `loopzero connect` → `git push` → app running with URL accessible.

### Required: Real fullstack deploy flow

Every load test deploy MUST execute the **complete pipeline**, exactly as a real user would:

```
Create tenant (via API)
  → TenantController reconciles (Gitea repo, namespace, ArgoCD, RBAC)
  → Wait for tenant Active

Create project (via API)
  → Environment provisioned

Upload REAL source bundle (built from fixture)
  → Git bundle with real code (hello-server or dogfood-monorepo fixture)

Submit build (via API)
  → Argo Workflow runs (clone → BuildKit → push image → sign)
  → GitOps commit to Gitea
  → ArgoCD syncs manifests

Wait for terminal state
  → Release ACTIVE
  → Pod Running
  → HTTP 200 on app URL

Verify app is reachable
  → GET https://{tenant}-{project}-{env}-production.{domain}
  → Assert HTTP 200 or expected response
```

### Fixtures

Three fixture types, matching real workloads:

| Fixture | Path | Type | Build time | Pod cost |
|---------|------|------|------------|----------|
| `hello-server` | `infra/tests/e2e/fixtures/hello-server/` | Node.js server | ~20s | ~30Mi |
| `dogfood-monorepo` | `infra/tests/e2e/fixtures/dogfood-monorepo/` | Turbo monorepo (API + Web + Worker) | ~40s | ~90Mi |
| `static-frontend` | New — Vite + React | Static (CDN delivery) | ~15s | 0 pods |

For scale tests, `hello-server` is the default (smallest, fastest, predictable). The `dogfood-monorepo` is used for a percentage of tenants to validate multi-app deploys.

### Bundle preparation

Each load test tenant needs a **real git bundle**. Pre-build once, reuse for all tenants:

```go
// PrepareFixtureBundle creates a real git bundle from a fixture directory.
// This runs ONCE before the load test, not per-tenant.
func PrepareFixtureBundle(fixturePath string) ([]byte, error) {
    // 1. Copy fixture to temp dir
    // 2. git init + git add + git commit
    // 3. git bundle create bundle.bin HEAD
    // 4. Return bundle bytes
}
```

The bundle is identical for all tenants — the `loopzero.yaml` project name is the only difference (set via upload metadata, not bundle content).

### Authentication for load tests

Use `X-Service-Auth` (internal service auth bypass) — already implemented in `seed_test.go`. This avoids needing N GitHub OAuth tokens.

For **each synthetic tenant**, the load test creates a unique identity:
- User: `loadtest-user-{i}` with email `loadtest-{i}@loadtest.loopzero.dev`
- Tenant: `loadtest-tn-{i}`
- Project: `loadtest-proj-{i}`

### Tenant provisioning (the critical difference)

**Old approach (wrong):** `INSERT INTO tenants` via SQL.

**New approach (correct):** Call `POST /api/v1/tenants` or equivalent bootstrap flow that:
1. Creates user + tenant in PostgreSQL
2. Creates Tenant CR on mgmt cluster
3. Waits for TenantController to reconcile:
   - Gitea source + gitops repos created
   - ArgoCD Application created
   - Build namespace created with RBAC + quotas
   - Runtime namespace created with NetworkPolicy + PSA
   - Harbor project created (if configured)
4. Tenant status reaches `Active`

**This is the ONLY way to create a tenant that can actually deploy.** The SQL seed is insufficient because the platform infrastructure (Gitea, ArgoCD, K8s namespaces) is never provisioned.

Time budget: ~30s per tenant (TenantController reconcile). For 500 tenants: ~25 min with parallelism of 10.

### Verification after deploy

Every deploy is verified by:
1. **Build state = SUCCEEDED** (polled via API)
2. **Release state = ACTIVE** (polled via API)
3. **HTTP reachability** — GET the app URL, assert 200
4. **Pod health** — GET /api/v1/projects/{name}/status, assert pods running

If any verification fails, the deploy counts as **failure** in the report.

### Cleanup

After each load test level:
1. `loopzero project delete` for each synthetic project (via API — triggers clean gitops removal + ArgoCD prune)
2. Wait for pods to terminate
3. Delete Tenant CRs (triggers TenantController cleanup)
4. Verify: no orphaned namespaces, no orphaned ArgoCD apps, no orphaned Gitea repos

Cleanup is done via API, not SQL — to validate the delete flow at scale too.

### k6 integration

The existing k6 suite (`infra/tests/k6/`) provides coordinated-omission-safe scheduling and Prometheus remote-write. The k6 scripts should be updated to:
1. Use real git bundles (built by init container, already in `job-template.yaml`)
2. Wait for build completion (SUCCEEDED, not just 202)
3. Verify app URL reachability
4. Report `deploy_e2e_duration` (upload → app live), not just `upload_duration`

### What the load test measures (changed)

| Metric | Old (fake) | New (real) |
|--------|:----------:|:----------:|
| Upload latency | Fake header → ~100ms | Real bundle → ~2s |
| Build latency | N/A (fails) | BuildKit compile → ~20-40s |
| Image push | N/A | Harbor push → ~5s |
| GitOps commit | N/A | Gitea push → ~1s |
| ArgoCD sync | N/A | Sync + health → ~30s |
| App reachable | N/A | HTTP 200 → ~5s |
| **Total e2e** | **~5s (meaningless)** | **~2-4min (real)** |

---

## Phases

### Phase 0 — Wire Everything + Real Load Test Harness (Week 1)

**Goal:** Activate all existing building blocks AND build a load test harness that exercises the real deploy flow.

#### 0.0 — Build real deploy load test harness

The current `DeploySimulator` in `cell_scale_test.go` uploads fake bundles and never completes a real deploy. Replace it with a harness that executes the full dogfood flow.

1. **`PrepareFixtureBundle()`** — Pre-build a real git bundle from `infra/tests/e2e/fixtures/hello-server/` (once, reusable)
2. **`ProvisionTenant()`** — Create user + tenant via API (not SQL), wait for TenantController reconcile (Tenant status = Active). Verify: Gitea repos exist, namespace exists, ArgoCD app exists
3. **`RealDeploy()`** — Upload real bundle → POST /builds → poll until SUCCEEDED → verify release ACTIVE → HTTP GET app URL → assert 200
4. **`VerifyAppLive()`** — GET `https://{tenant}-{project}-production.dev.loopzero.example` → assert HTTP 200
5. **`CleanupTenant()`** — `DELETE /api/v1/projects/{name}` → wait for pods gone → delete Tenant CR → verify no orphans
6. Update k6 scripts (`infra/tests/k6/scripts/`) to use real bundles and verify app reachability
7. Update k6 `job-template.yaml` init container to build a real git bundle from the hello-server fixture (already partially there)

**DoD:** Running `TestL1_10Tenants` creates 10 real tenants, deploys real apps, verifies all 10 URLs return HTTP 200, then cleans up. No fake bundles. No SQL seeds.

#### 0.1 — Activate controller binary extraction

1. Set `LOOPZERO_DISABLE_LEGACY_LOOPS=true` in API deployment (Pulumi env vars)
2. Deploy `loopzero-controller` as separate Deployment (1 replica, leader election via generation fencing)
3. Verify: API serves HTTP only, controller owns all 6 loops
4. Verify: Grafana shows controller as leader, API as non-leader
5. Rollback plan: set `LOOPZERO_DISABLE_LEGACY_LOOPS=false`, restart API

**DoD:** `loopzero deploy` works end-to-end with loops running in controller, not API.

#### 0.2 — Migrate loops from sync.Map to PG claims

1. Refactor `BuildReconcileLoop.processBuilds()` to use `ClaimStore.Claim()` instead of `active.LoadOrStore()`
2. Refactor `ReleaseConvergenceLoop` to use `ClaimStore.Claim()` instead of in-memory dedup
3. Keep `sync.Map` as fallback behind feature flag `LOOPZERO_USE_PG_CLAIMS=true`
4. Run existing loop tests — zero regressions
5. Run `TestAPI_MultiReplica_NoDuplicateProcessing` against refactored loops

**DoD:** Loops use PG claims. Two controller replicas process builds without duplication.

#### 0.3 — Wire DynamicRouter + K8sCellStore

1. In `api/cmd/main.go`: replace `PassthroughRouter` with `DynamicRouter` initialized from `K8sCellStore`
2. Fallback: if no ClusterCredential CRDs found, DynamicRouter returns `cell-default` (backward compatible)
3. Wire `PlacementEngine` (RoundRobin) for new tenant creation
4. Wire `CrossCellAggregator` in status/list endpoints
5. Run existing API tests — zero regressions

**DoD:** API uses DynamicRouter. New tenants get cell assignment via PlacementEngine.

#### 0.4 — Enable admission control

1. Set `LOOPZERO_ADMISSION_CONTROL=true` in production
2. Verify: build requests respect cell-level (20) and per-tenant (3) limits
3. Verify: HTTP 429 with Retry-After header on quota exceeded
4. Verify: Prometheus metrics `loopzero_build_admission_rejected_total` incrementing

**DoD:** Admission gate active in production.

#### 0.5 — Deploy controller Helm chart

1. Create `infra/tilt/controller.yaml` (or add to existing Helm chart) for controller Deployment
2. Resource limits: 128Mi request / 512Mi limit (matches current)
3. Health check on `:9091/healthz`
4. Liveness/readiness probes
5. ServiceMonitor for Prometheus scrape

**DoD:** Controller is a first-class Kubernetes workload with monitoring.

---

### Phase 1 — Single-Cell Hardening + L0-L2 (Week 2)

**Goal:** Validate single cell to its limits with real load.

#### 1.1 — L0 Baseline

Run `TestL0_Baseline` against production (dev stack):
- Record: deploy latency, PG pool utilization, ArgoCD sync time, node memory/CPU
- Annotate Grafana with baseline values
- **Time:** 1 hour

#### 1.2 — L1 (10 tenants)

Run all L1 scenarios from load-test-plan.md — **every deploy must produce a live, reachable app:**
- 1.1 Sequential deploys (10 tenants, serial) → **10 apps live with HTTP 200**
- 1.2 Concurrent deploys (10 tenants, simultaneous) → **1 active per env, others queued (409), all eventually live**
- 1.3 Webhook burst (10 push events in 1s) → **dedup correct, 10 builds (not 20), all apps reachable**
- 1.4 Superseded push (2 rapid pushes) → **first cancelled, second succeeds, app serves latest code**
- 1.5 Rollback under load → **rollback succeeds, app reverts to previous version (verified by response content)**

**Verification per deploy:**
1. Build state = SUCCEEDED
2. Release state = ACTIVE
3. `GET https://{tenant}-loadtest-{i}-production.dev.loopzero.example` returns HTTP 200
4. Response body matches expected fixture output (e.g., `{"status":"ok"}` for hello-server)

**Chaos injection at L1:**
- Kill API pod during deploy → verify controller continues processing → app goes live
- Kill controller pod → verify leader re-election via generation fencing → no stuck builds
- PG connection spike → verify circuit breaker trips + recovers → next deploy succeeds

**Cleanup:** Delete all 10 projects via API. Verify: zero orphaned namespaces, zero orphaned ArgoCD apps, zero orphaned Gitea repos.

**DoD:** All 5 scenarios pass. 10 apps live and verified. Zero stuck builds. All chaos scenarios recover. Cleanup leaves zero orphans. **Time:** 4 hours

#### 1.3 — L2 (50 tenants)

**Infra scaling:**
- PG: bump to `db-s-1vcpu-4gb` (max_connections=100) via Pulumi
- Build MaxConcurrent: 10 → 20
- Build cluster: 2 → 4 nodes

Run all L2 scenarios — **every deploy produces a live, verified app:**
- 2.1 Sustained deploys (50 tenants, 5 deploys/min, 30min) → **50 apps live, verified every 5min**
- 2.2 Burst (50 simultaneous deploys) → **all 50 complete within 15min, all URLs return 200**
- 2.3 Webhook storm (50 push events in 2s) → **dedup + serialization correct, all apps eventually live**
- 2.4 Mixed operations (deploy + rollback + secret-set concurrent) → **no deadlocks, rollback app serves previous version**

**Verification:** Same as L1 — every deploy checked for BUILD SUCCEEDED + RELEASE ACTIVE + HTTP 200 on app URL.

**Chaos injection at L2:**
- PG failover (kill primary) during 20 active deploys → recovery < 2min, all 20 eventually succeed
- Build cluster node drain → builds reschedule to remaining nodes, complete successfully
- Network partition on controller → new leader elected, no stuck builds, all deploys complete

**Cleanup:** Delete all 50 projects via API. Verify zero orphans (namespaces, ArgoCD, Gitea, Harbor).

**DoD:** All scenarios pass. 50 apps verified live. No metric in Red zone. Lock conflicts resolve within 5s. Zero orphans after cleanup. **Time:** 1 day

---

### Phase 2 — Push Single Cell to Breaking Point + L3-L4 (Weeks 3-4)

**Goal:** Find the real breaking point of one cell. Fix bottlenecks. Document limits.

#### 2.1 — L3 (100 tenants)

**Infra scaling:**
- API replicas: 2 → 4 (HPA: min=2, max=6, target CPU 70%)
- Runtime cluster: 2 → 4 nodes
- Mgmt cluster: 3 → 4 nodes
- ArgoCD controller memory: 512Mi → 1Gi
- ArgoCD status-processors: 20 → 40, operation-processors: 10 → 20
- Gitea memory: 512Mi → 1Gi

Run all L3 scenarios:
- 3.1 Sustained load (100 tenants, 10 deploys/min, 1 hour)
- 3.2 Noisy neighbor (1 tenant doing 20 rapid deploys)
- 3.3 ArgoCD pressure (100 apps, 50 syncing)
- 3.4 Gitea pressure (200 repos, concurrent git push)
- 3.5 SSE saturation (100 concurrent `loopzero deploy --watch`)

**Decision point:** Per-project ArgoCD Applications? If sync time > 5min → split.

**Chaos injection at L3:**
- All 6 existing chaos scenarios under load
- ArgoCD OOM kill → restart + resync
- Gitea disk full → reject push, recover after cleanup
- SSE connection storm (200 clients connecting/disconnecting rapidly)

**DoD:** 1 hour sustained load, stable metrics. Bottleneck identified and documented. **Time:** 2 days

#### 2.2 — L4 (500 tenants)

**Infra scaling:**
- PG: scale to HA (primary + 2 replicas), max_connections=200
- PgBouncer between API and PG (connection multiplexing)
- Build cluster: 4 → 8 nodes, MaxConcurrent: 20 → 50
- Runtime cluster: 4 → 8 nodes
- Mgmt cluster: 4 → 6 nodes
- ArgoCD: sharding if L3 decision requires it

Run all L4 scenarios:
- 4.1 Sustained high (500 tenants, 50 deploys/min, 2 hours)
- 4.2 Burst storm (200 simultaneous deploys)
- 4.3 Webhook flood (500 push events in 5s)
- 4.4 PG failover under load (kill primary during 50 active deploys)
- 4.5 Build cluster saturation (80 concurrent builds)

**Chaos injection at L4:**
- Everything from L3 +
- Simultaneous controller + API crash → full recovery < 3min
- PG pool exhaustion (reject all connections) → circuit breaker → graceful degradation → recovery
- Kill 50% of build nodes mid-build → builds retry on remaining nodes
- Corrupt a Gitea repo → tenant isolated, others unaffected

**DoD:** System degrades gracefully (no crash, no data loss). Cell capacity documented with evidence. **Time:** 1 week

---

### Phase 3 — Multi-Cell Activation (Weeks 5-6)

**Goal:** Provision real second cell. Validate cell isolation and routing.

#### 3.1 — Provision second cell

1. Wire `createCell()` in Pulumi `index.ts` for a second cell (`cell-sfo3-02`)
2. Run `pulumi up` — second DOKS cluster + ArgoCD + Traefik + cert-manager provisioned
3. Create `ClusterCredential` CR for cell-sfo3-02 on mgmt cluster
4. Verify: `K8sCellStore` discovers new cell, `DynamicRouter` routes to it
5. Verify: Grafana shows both cells on cell-saturation dashboard

**DoD:** Two real cells operational. kubectl shows workloads in both clusters.

#### 3.2 — Multi-cell routing validation

1. Create 10 tenants → verify PlacementEngine distributes across 2 cells (round-robin)
2. Deploy to tenant in cell-sfo3-01 → verify build + deploy uses correct cell
3. Deploy to tenant in cell-sfo3-02 → verify build + deploy uses correct cell
4. Verify CrossCellAggregator returns merged results for `loopzero project list`

**DoD:** Tenants in different cells deploy independently. Cross-cell queries work.

#### 3.3 — Cell isolation chaos

1. Kill cell-sfo3-02 entirely (drain all nodes) → verify cell-sfo3-01 unaffected
2. Kill ArgoCD in cell-sfo3-01 → verify cell-sfo3-02 continues syncing
3. Network partition between cells → verify independent operation
4. Kill PG (shared) → verify both cells degrade gracefully, recover together

**DoD:** Cell failure does not propagate. Blast radius is one cell.

#### 3.4 — Remove t.Skip() from multi-cell load tests

1. Enable `TestL4_500Tenants_MultiCell` (500 tenants across 2 cells)
2. Enable `TestL5_1000Tenants_MultiCell` (1000 tenants across 2+ cells)
3. Run L4 multi-cell test

**DoD:** Multi-cell load tests pass with real cells. **Time:** 2 weeks

---

### Phase 4 — Scale to 5,000 Tenants (Weeks 7-9)

**Goal:** L5 — validate multi-cell at 1,000-5,000 tenants. Find the next bottleneck.

#### 4.1 — Infrastructure for 5,000 tenants

At 500 tenants/cell: **10 cells needed**.

| Component | Per cell | Total (10 cells) |
|-----------|---------|-------------------|
| Runtime cluster | 8 nodes (medium) | 80 nodes |
| ArgoCD | Sharded (2 replicas) | 20 replicas |
| Traefik | 2 replicas | 20 replicas |
| Prometheus agent | 1 replica | 10 replicas |
| PG (shared) | — | HA + read replicas + PgBouncer |
| Gitea (shared) | — | 10,000 repos. HA or sharding decision required |
| Build cluster (shared) | — | 16 nodes (medium) |

#### 4.2 — Provision cells 3-10

1. Parameterize `createCell()` for N cells via Pulumi config array
2. Provision cells incrementally (2 → 4 → 8 → 10)
3. Validate ClusterCredential discovery for each new cell
4. Validate DynamicRouter sees all cells

#### 4.3 — L5 scenarios

| # | Scenario | Load | Duration | Pass Criteria |
|---|----------|------|----------|---------------|
| 5.1 | 2-cell deployment | 1,000 tenants across 2 cells | 4 hours | Both cells independent |
| 5.2 | Cell isolation | Kill Cell A entirely | 30min | Zero cross-cell impact |
| 5.3 | 5-cell deployment | 2,500 tenants across 5 cells | 4 hours | < 1% failure rate |
| 5.4 | 10-cell deployment | 5,000 tenants across 10 cells | 4 hours | < 1% failure rate |
| 5.5 | Cross-cell observability | Prometheus federation | 1 hour | Unified dashboards work |
| 5.6 | Cell addition under load | Add cell-11 while 5,000 tenants active | 30min | New tenants route to new cell |

**Chaos injection at L5:**
- Kill 3 cells simultaneously → other 7 unaffected
- PG failover during 5,000-tenant load → recovery < 5min
- Build cluster node failure during 100 concurrent builds → retry succeeds
- Gitea OOM → deploy queue grows, no data loss, recovery after restart
- CF Tunnel failure → traffic failover (if multi-tunnel configured)

**Architectural decision points at L5:**
- Gitea at 10,000 repos: HA? Sharding? Bare git + S3?
- PG at 5,000 tenants: partitioning by cell_id? Read replicas for queries?
- ArgoCD at 5,000 apps: sharding sufficient? Per-cell ArgoCD instance?
- Build pipeline at 100 concurrent: Argo Workflows tuning? sharding by namespace?

**DoD:** 10 cells running. 5,000 tenants stable for 4 hours. Cell failure contained. **Time:** 3 weeks

---

### Phase 5 — Scale to 50,000 Applications (Weeks 10-16)

**Goal:** L6 — validate 50,000 fullstack apps. Multi-region.

#### 5.1 — Architecture for 50,000

50,000 tenants × 500/cell = **100 cells**.

| Component | Configuration |
|-----------|--------------|
| Regions | 2 (SFO3 + NYC3) |
| Cells per region | 50 |
| Runtime nodes per cell | 8 (medium) → 800 nodes total |
| PG | HA, 3 nodes, partitioned by cell_id, PgBouncer (200+ conns) |
| Gitea | Sharded or replaced with bare git + S3 (100,000 repos) |
| ArgoCD | Per-cell instance (50,000 apps total) |
| Build cluster | Per-region, 32 nodes each, Argo Workflows sharded by namespace |
| Prometheus | Per-cell agent → Thanos or Mimir for federation |
| CF Tunnel | Per-region (geo-routing via Cloudflare) |

#### 5.2 — Progressive scaling (10k → 25k → 50k)

| Step | Tenants | Cells | Duration | Pass Criteria |
|------|---------|-------|----------|---------------|
| 5.2.1 | 10,000 | 20 | 4 hours | < 0.5% failure, deploy p99 < 10min |
| 5.2.2 | 25,000 | 50 | 8 hours | < 0.5% failure, deploy p99 < 10min |
| 5.2.3 | 50,000 | 100 | 12 hours | < 0.5% failure, deploy p99 < 10min |

Between each step:
1. Analyze bottlenecks from Grafana
2. Fix identified issues
3. Document actual vs projected capacity
4. Tune ArgoCD/Argo Workflows/PG per section findings

#### 5.3 — Full chaos suite at 50k

| # | Scenario | Pass Criteria |
|---|----------|---------------|
| C1 | Kill 10 cells (10%) simultaneously | 90% of tenants unaffected |
| C2 | PG failover during peak load | Recovery < 5min, zero data loss |
| C3 | Complete region failure (SFO3 down) | NYC3 tenants unaffected, SFO3 tenants degrade |
| C4 | Build cluster saturation (all 64 nodes busy) | Queue grows, admission rejects, no crash |
| C5 | Gitea unavailable for 5 minutes | Deploys queue, resume after recovery |
| C6 | CF Tunnel failure per-region | Traffic reroutes via other region |
| C7 | Controller leader election storm (rapid failover 10x) | Fencing prevents duplicate processing |
| C8 | PG pool exhaustion across all API pods | Circuit breaker trips, reads work, mutations queue |
| C9 | Webhook flood (10,000 events in 10s) | Dedup correct, admission controls queue depth |
| C10 | Corrupt ArgoCD state in 1 cell | Cell isolated, rebuild from gitops source of truth |

**DoD:** 50,000 fullstack apps running across 100 cells in 2 regions. All chaos scenarios pass. Capacity model documented with evidence. **Time:** 6 weeks

---

## Success Criteria

**All latencies are REAL end-to-end** (upload → build → image push → gitops → ArgoCD sync → pod running → HTTP 200). Not upload-only.

| Level | Tenants | Apps | Cells | Deploy e2e p99 | Failure Rate | Apps Verified Live | Chaos Pass |
|-------|---------|------|-------|----------------|--------------|:------------------:|:----------:|
| L0 | 2 | 4 | 1 | Baseline (~4min) | 0% | 4 | — |
| L1 | 10 | 20 | 1 | < 6min | 0% | 20 | 3 scenarios |
| L2 | 50 | 100 | 1 | < 6min | 0% | 100 | 6 scenarios |
| L3 | 100 | 200 | 1 | < 8min | < 0.5% | 200 | 9 scenarios |
| L4 | 500 | 1,000 | 1 | < 10min | < 1% | 1,000 | 12 scenarios |
| L4-MC | 500 | 1,000 | 2 | < 10min | < 1% | 1,000 | 15 scenarios |
| L5 | 5,000 | 10,000 | 10 | < 10min | < 1% | 10,000 | 18 scenarios |
| L6 | 50,000 | 100,000 | 100 | < 10min | < 0.5% | 100,000 | 28 scenarios (C1-C10 + all prior) |

**"Apps Verified Live"** = each app URL returns HTTP 200 after deploy completes. This is the dogfood guarantee — if it's not live, it's not a successful deploy.

---

## Cost Projection

| Level | Cells | Nodes | DO Cost/mo (est.) |
|-------|-------|-------|------------------|
| L0-L2 (current) | 1 | 7 | ~$350 |
| L3 | 1 | 12 | ~$800 |
| L4 | 1 | 22 | ~$2,500 |
| L5 | 10 | ~100 | ~$8,000 |
| L6 (50k) | 100 | ~900 | ~$60,000 |

**Note:** L5-L6 are load test costs, not steady-state. Tests run for hours, not months. Actual cost for a 4-hour L6 test with on-demand nodes: ~$350 (prorated hourly).

---

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Gitea becomes bottleneck at 10k+ repos | High | Pre-L5 decision: sharding, HA, or replace with bare git+S3 |
| PG shared instance can't handle 50k tenants | High | Partitioning by cell_id (ADR-0008). If insufficient: PG per region |
| ArgoCD memory at 500+ apps per cell | Medium | Sharding + per-cell instance (already planned) |
| Build cluster Argo Workflows at 100+ concurrent | Medium | Namespace sharding + pod GC tuning |
| Cost of 100-cell test | Medium | Use on-demand nodes, tear down after test, prorated hourly billing |
| CF API rate limits at 100 cells | Low | Batch DNS operations, cache aggressively |

---

## Execution Timeline

```
Week 1:   Phase 0 — Wire everything (controller extraction, PG claims, DynamicRouter, admission)
Week 2:   Phase 1 — L0 baseline + L1 (10 tenants) + L2 (50 tenants) + chaos
Weeks 3-4: Phase 2 — L3 (100 tenants) + L4 (500 tenants) + chaos + bottleneck fixes
Weeks 5-6: Phase 3 — Provision 2nd cell + multi-cell validation + L4-MC
Weeks 7-9: Phase 4 — Scale to 10 cells + L5 (5,000 tenants) + chaos
Weeks 10-16: Phase 5 — Scale to 100 cells + L6 (50,000 tenants) + full chaos suite
```

**Total: ~16 weeks from today to 50,000 validated fullstack applications.**

---

## Definition of Done (Final)

The plan is complete when ALL of the following are true simultaneously:

1. 50,000 fullstack applications deployed across 100 cells in 2 regions
2. **All 100,000 apps verified live** — HTTP 200 on every app URL (not just build SUCCEEDED)
3. Deploy e2e p99 < 10 minutes under sustained load (12 hours) — measured from upload to app reachable
4. Failure rate < 0.5% during sustained load
5. All 28 chaos scenarios pass (C1-C10 + per-level chaos)
6. Cell failure does not propagate (blast radius = 1 cell)
7. Capacity model documented with **measured** (not projected) values per cell
8. PG, ArgoCD, Argo Workflows tuned with validated parameters
9. Controller runs independently from API (zero singleton dependency)
10. Admission control active (build gate + circuit breaker + rate limiter)
11. **Zero fake data** — every tenant provisioned via API (not SQL), every deploy uses real bundles, every app runs real code
12. **Cleanup verified** — after test, zero orphaned namespaces, ArgoCD apps, Gitea repos, Harbor projects
13. All load test reports archived in `docs/validation/load-reports/`

---

## References

- [Load Test Plan (L0-L7)](load-test-plan.md)
- [Cell Capacity Budget](../capacity/cell-budget.md)
- [ADR-0008: Cell-Based Architecture](../meetings/adr/0008-cell-based-architecture.md)
- [API Scaling Runbook](../runbooks/api-scaling.md)
- [Cell Operations Runbook](../runbooks/cell-operations.md)
