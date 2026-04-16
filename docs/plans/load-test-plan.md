# Load Test Plan — 10 to 500,000 Tenants

**Date:** 2026-04-09
**Status:** Draft
**Owner:** SRE + Platform Engineering
**Prerequisite:** MEET-045 complete (all reliability primitives deployed)

---

## Current Infrastructure Limits

| Resource | Current Config | Hard Limit | Bottleneck At |
|----------|---------------|------------|---------------|
| PG connections | 25 per API pod (2 pods = 50) | DO Managed PG: 97 max | ~80 concurrent ops |
| PG advisory locks | 1 lock per (tenant, project, env) | Bound by connections | ~50 concurrent deploys |
| API replicas | 2 | HPA not configured | CPU/mem saturation |
| Build concurrency | MaxConcurrent=10 | Argo parallelism | 10 simultaneous builds |
| Release convergence | MaxConcurrent=10 | — | 10 parallel verifications |
| SSE streams | 200 max | — | 200 concurrent `loopzero deploy --watch` |
| ArgoCD Applications | 1 per tenant-environment | ArgoCD controller mem | ~200 apps (8GB RAM) |
| Gitea repos | 2 per tenant (source + gitops) | Gitea single instance | ~500 repos (disk I/O) |
| Build cluster nodes | 2 × small (2vCPU/4GB) | — | ~20 concurrent build pods |
| Runtime cluster nodes | 2 × small (2vCPU/4GB) | — | ~40 tenant pods |
| Mgmt cluster nodes | 3 × small (2vCPU/4GB) | — | Already 80% memory |
| Cloudflare API | 4 req/s (rate limited) | 1200 req/5min | ~300 domain ops/5min |
| DO Spaces | No limit | Bandwidth cost | — |
| DO Container Registry | Professional tier | 5 repos unlimited tags | — |

---

## Test Levels

### Notation

- **Synthetic tenant**: API call to create tenant + project + environment (no real GitHub repo)
- **Deploy**: Full pipeline — upload bundle → build → push image → gitops → ArgoCD sync → activate
- **Webhook burst**: Simulated GitHub push events hitting the webhook endpoint
- **Steady state**: Continuous low-rate deploys (~1/min) over extended period

---

## Level 0 — Baseline (2 tenants, CURRENT)

**Goal:** Establish baseline metrics with current production data.

**What to measure:**
- Single deploy end-to-end latency (upload → live)
- PG pool utilization during deploy
- ArgoCD sync time
- Memory/CPU per node during idle + deploy

**DoD:** Baseline recorded in Grafana annotations. All metrics have a reference value.

**Estimated time:** 1 hour

---

## Level 1 — Validation (10 tenants)

**Goal:** Validate all primitives work correctly with minimal concurrency.

### Infrastructure Changes
None — current infra supports this.

### Test Scenarios

| # | Scenario | Load | Duration | Pass Criteria |
|---|----------|------|----------|--------------|
| 1.1 | Sequential deploys | 10 tenants, 1 deploy each, serial | 30min | All succeed, zero duplicates |
| 1.2 | Concurrent deploys | 10 tenants, all deploy simultaneously | 10min | 1 active per env, others queued (409) |
| 1.3 | Webhook burst | 10 push events in 1 second | 5min | Dedup works, 10 builds (not 20) |
| 1.4 | Superseded push | 2 rapid pushes to same repo | 5min | First build cancelled, second succeeds |
| 1.5 | Rollback under load | Deploy + rollback while build running | 5min | Rollback succeeds, no stuck states |

### Metrics to Capture
- `loopzero_build_queue_depth` (should not exceed 10)
- `loopzero_lock_conflicts_total` (expected: some during 1.2)
- `loopzero_api_request_duration_seconds` p99
- Build end-to-end latency per tenant
- PG pool active connections peak

### Tools
```bash
# Create synthetic tenants
for i in $(seq 1 10); do
  curl -X POST https://api.dev.loopzero.example/api/v1/projects \
    -H "Authorization: Bearer $TOKEN" \
    -d "{\"name\":\"loadtest-$i\",\"framework\":\"node\"}"
done

# Concurrent deploy simulation (k6 or custom Go script)
# Each tenant: upload bundle → POST /builds → SSE watch
```

**DoD:** All 5 scenarios pass. Zero stuck builds. Zero duplicate releases.

**Estimated time:** 4 hours

---

## Level 2 — Concurrency (50 tenants)

**Goal:** Validate concurrency primitives under moderate load.

### Infrastructure Changes
- PG pool: 25 → 50 per pod (`DB_MAX_CONNS=50`)
- Build MaxConcurrent: 10 → 20
- Build cluster: 2 → 4 nodes (handle 20 concurrent builds)

### Test Scenarios

| # | Scenario | Load | Duration | Pass Criteria |
|---|----------|------|----------|--------------|
| 2.1 | Sustained deploys | 50 tenants, 5 deploys/min | 30min | Queue depth < 20, zero failures |
| 2.2 | Burst | 50 simultaneous deploys | 15min | All complete within 15min |
| 2.3 | Webhook storm | 50 push events in 2 seconds | 10min | Dedup+serialization correct |
| 2.4 | Mixed operations | Deploy + rollback + secret-set concurrent | 20min | No deadlocks, no 500s |

### Metrics to Capture
- Lock acquisition latency (p50, p95, p99)
- Lock conflict rate
- Build queue wait time
- ArgoCD sync queue depth
- PG connection pool saturation %

### Critical Thresholds
| Metric | Yellow | Red |
|--------|--------|-----|
| Lock acquisition p99 | >2s | >5s |
| Build queue depth | >15 | >30 |
| PG pool saturation | >70% | >90% |
| API response p99 | >1s | >3s |

**DoD:** All scenarios pass. No metric in Red zone. Lock conflicts resolve within 5s.

**Estimated time:** 1 day

---

## Level 3 — Scale (100 tenants)

**Goal:** Find the first real bottleneck in the current architecture.

### Infrastructure Changes
- API replicas: 2 → 4 (HPA: min=2, max=6, target CPU 70%)
- Runtime cluster: 2 → 4 nodes
- Mgmt cluster: 3 → 4 nodes (ArgoCD memory)
- ArgoCD: increase controller memory limit 512Mi → 1Gi
- Gitea: increase memory limit 512Mi → 1Gi

### Test Scenarios

| # | Scenario | Load | Duration | Pass Criteria |
|---|----------|------|----------|--------------|
| 3.1 | Sustained load | 100 tenants, 10 deploys/min | 1 hour | Zero failures, stable latency |
| 3.2 | Noisy neighbor | 1 tenant doing 20 rapid deploys | 15min | Other tenants unaffected |
| 3.3 | ArgoCD pressure | 100 ArgoCD apps, 50 syncing | 30min | Sync time < 2min per app |
| 3.4 | Gitea pressure | 200 repos, concurrent git push | 20min | No Gitea OOM or slowdown |
| 3.5 | SSE saturation | 100 concurrent `loopzero deploy --watch` | 30min | All streams receive events |

### Architectural Decision Point

**AT THIS LEVEL:** Evaluate whether per-project ArgoCD Applications are needed.

If 100 tenants with shared Application cause:
- Sync time > 5min → **P0: split to per-project**
- Degraded notifications blocking others → **P0: split to per-project**
- Healthy behavior → defer to Level 5

**DoD:** 1 hour sustained load with stable metrics. Bottleneck identified and documented.

**Estimated time:** 2 days

---

## Level 4 — Stress (500 tenants)

**Goal:** Find the breaking point. Expect failures — measure degradation.

### Infrastructure Changes
- **Per-project ArgoCD Applications** (if not already done at L3)
- PG pool: 50 → 100 per pod
- DO Managed PG: scale to 4 nodes (HA + read replicas)
- Build cluster: 4 → 8 nodes
- Runtime cluster: 4 → 8 nodes
- Mgmt cluster: 4 → 6 nodes
- Build MaxConcurrent: 20 → 50
- Introduce PgBouncer between API and PG (connection multiplexing)

### Test Scenarios

| # | Scenario | Load | Duration | Pass Criteria |
|---|----------|------|----------|--------------|
| 4.1 | Sustained high | 500 tenants, 50 deploys/min | 2 hours | <1% failure rate |
| 4.2 | Burst storm | 200 simultaneous deploys | 30min | All complete, degradation graceful |
| 4.3 | Webhook flood | 500 push events in 5 seconds | 15min | Dedup correct, queue bounded |
| 4.4 | PG failover under load | Kill PG primary during 50 active deploys | 15min | Recovery < 2min, zero lost deploys |
| 4.5 | Build cluster saturation | 80 concurrent builds | 30min | Queue grows linear, no crash |

### Expected Bottlenecks
- Gitea single instance (500 tenants = 1000 repos)
- ArgoCD controller memory (500 apps)
- Advisory lock contention at high concurrency
- S3 upload bandwidth

### Mitigation Options to Evaluate
| Bottleneck | Solution | Complexity |
|------------|----------|-----------|
| Gitea | Gitea HA or switch to bare git + S3 | High |
| ArgoCD memory | Sharding (ApplicationSet + cluster sharding) | Medium |
| Advisory locks | Lock partitioning by project hash | Medium |
| S3 bandwidth | Multi-region Spaces or direct-to-registry | Low |

**DoD:** System degrades gracefully (no crash, no data loss). Bottleneck identified with numbers.

**Estimated time:** 1 week

---

## Level 5 — Multi-Cell (1,000–5,000 tenants)

**Goal:** Validate multi-cell architecture. Single cluster can't hold this.

### Architectural Changes Required

```
              ┌─────────────────────────────────────────────┐
              │         SHARED CONTROL PLANE                 │
              │  Global Router (CF Worker + KV)              │
              │  PostgreSQL (partitioned by cell_id)         │
              │  Gitea (shared)    Argo Workflows (shared)  │
              └──────┬──────────────┬──────────────┬────────┘
                     │              │              │
         ┌───────▼──────┐  ┌──────▼──────┐  ┌───▼────────────┐
         │ Cell A        │  │ Cell B       │  │ Cell C          │
         │ K8s runtime   │  │ K8s runtime  │  │ K8s runtime     │
         │ ArgoCD        │  │ ArgoCD       │  │ ArgoCD          │
         │ Traefik       │  │ Traefik      │  │ Traefik         │
         │ Prom agent    │  │ Prom agent   │  │ Prom agent      │
         └──────────────┘  └─────────────┘  └────────────────┘
```

### Per-Cell Capacity Target
- 500 tenants per cell (validated at Level 4)
- **Per cell:** ArgoCD, Traefik, cert-manager, Prometheus agent, K8s workload cluster
- **Shared (control plane):** PostgreSQL (partitioned by cell_id per ADR-0008), Gitea, Argo Workflows, Container Registry, CF DNS, GitHub App
- **Note:** Earlier drafts of this plan listed "Independent PG per cell." Per ADR-0008 and the scaling roadmap (`docs/roadmap/05-cell-provisioning/roadmap.md`), PG is shared on the control plane with table partitioning by cell_id. PG per cell is a future option evaluated only if L5+ data shows shared PG as bottleneck (requires T3 meeting).

### Infrastructure Per Cell
| Component | Spec | Shared? |
|-----------|------|---------|
| Runtime cluster | 4 × medium nodes + autoscale to 16 | No — per cell |
| ArgoCD | Sharded controller (2 replicas) | No — per cell |
| Traefik | 2 replicas | No — per cell |
| Prometheus agent | 1 replica (remote-write to central) | No — per cell |
| PostgreSQL | HA (primary + 2 replicas) | **Yes — shared, partitioned by cell_id** |
| Gitea | Single instance (500 repos max per cell's tenants) | **Yes — shared** |
| Build cluster | 4 × medium nodes + autoscale to 8 | **Yes — shared** |

### Test Scenarios

| # | Scenario | Load | Duration | Pass Criteria |
|---|----------|------|----------|--------------|
| 5.1 | 2-cell deployment | 1000 tenants across 2 cells | 4 hours | Both cells independent |
| 5.2 | Cell isolation | Kill Cell A, Cell B unaffected | 30min | Zero cross-cell impact |
| 5.3 | Tenant migration | Move 50 tenants from Cell A → B | 1 hour | Zero downtime |
| 5.4 | Cross-cell observability | Grafana + Prometheus federation | 1 hour | Unified dashboards work |

### New Components Needed
- **Cell Router**: CF Worker routes requests to correct cell based on tenant
- **Cell Registry**: Mapping tenant → cell in global PG or CF KV
- **Tenant Migration Tool**: Move tenant data + gitops + releases between cells
- **Federated Prometheus**: Cross-cell metrics aggregation

**DoD:** 2 cells running independently. Cell failure doesn't affect other cell. Tenant migration works.

**Estimated time:** 4-6 weeks

---

## Level 6 — Regional (5,000–50,000 tenants)

**Goal:** Multi-region deployment. Latency matters.

### Architectural Changes
- Cells deployed in multiple DO regions (SFO3, NYC3, AMS3, SGP1)
- Geo-routing via Cloudflare (latency-based)
- Global PG for billing/auth (CockroachDB or PG with logical replication)
- Per-region build clusters (avoid cross-region image push)
- Per-region runtime clusters
- CDN for static assets (already via CF/Spaces)

### Infrastructure (per region)
| Region | Cells | Tenants | Total Nodes |
|--------|-------|---------|-------------|
| SFO3 | 10 | 5,000 | ~120 |
| NYC3 | 10 | 5,000 | ~120 |
| AMS3 | 5 | 2,500 | ~60 |
| SGP1 | 5 | 2,500 | ~60 |

### Test Scenarios

| # | Scenario | Load | Pass Criteria |
|---|----------|------|--------------|
| 6.1 | Multi-region deploy | Tenants in SFO3 + NYC3 | Deploy latency < 5min in both |
| 6.2 | Region failover | Kill SFO3, traffic to NYC3 | Failover < 30s via CF |
| 6.3 | Cross-region auth | Login in SFO3, deploy in NYC3 | JWT works cross-region |
| 6.4 | Build cache hit rate | Shared registry, local cache | Cache hit > 60% |

**DoD:** Multi-region operational. Region failure handled by CF routing.

**Estimated time:** 2-3 months

---

## Level 7 — Enterprise (50,000–500,000 tenants)

**Goal:** Hyperscale. Architecture is fundamentally different.

### Architectural Changes
- **Control plane as a service**: API becomes stateless, scales horizontally
- **Build-as-a-Service**: Dedicated build clusters per customer tier
- **Sharded state**: PG per cell, global CockroachDB for routing + billing
- **Event-driven**: Replace polling with NATS/Kafka for build events
- **GitOps at scale**: Replace Gitea with managed Git (GitHub/GitLab integration direct)
- **ArgoCD per customer**: Dedicated ArgoCD for Enterprise tier, shared for Starter/Pro

### Capacity Model

| Tier | Tenants | Architecture | Monthly Cost (est.) |
|------|---------|-------------|-------------------|
| Starter (Free) | 400,000 | Shared cells, 800 cells × 500 | ~$960K/mo |
| Pro ($29/mo) | 80,000 | Dedicated runtime, shared build | ~$480K/mo |
| Team ($79/mo) | 15,000 | Dedicated cell per 50 tenants | ~$360K/mo |
| Enterprise | 5,000 | Dedicated clusters | Custom |
| **Total** | **500,000** | | **~$1.8M/mo** |

### Key Engineering Challenges
1. **Tenant placement**: Which cell for a new tenant? (bin packing)
2. **Hot tenant detection**: Identify + isolate noisy tenants automatically
3. **Zero-downtime cell upgrades**: Rolling upgrades across 800+ cells
4. **Observability at scale**: Can't federate 800 Prometheus instances
5. **Cost optimization**: Right-size cells, spot instances for builds
6. **Billing integration**: Usage metering across cells

### Test Focus
Not individual scenario tests — focus on:
- **Capacity planning accuracy**: Predicted vs actual resource per tenant
- **Cell provisioning time**: New cell ready in < 10min
- **Tenant onboarding latency**: Signup → first deploy < 5min
- **MTTR per failure type**: Catalog + measure

**DoD:** Architecture validated via capacity model + 10-cell pilot. Cost model accurate to ±20%.

**Estimated time:** 6-12 months

---

## Test Infrastructure

### Load Generator

Custom Go tool (`infra/tests/load/`) that:
1. Creates synthetic tenants via API
2. Uploads pre-built source bundles
3. Triggers deploys
4. Watches SSE for completion
5. Verifies app is reachable
6. Records latency per stage
7. Reports metrics to Prometheus (push gateway)

```go
// loadtest/config.go
type LoadTestConfig struct {
    Tenants         int           // Number of synthetic tenants
    DeploysPerMin   int           // Target deploy rate
    Duration        time.Duration // Test duration
    BurstSize       int           // Concurrent deploys in burst mode
    WebhookBurst    int           // Simulated webhook events
    APIEndpoint     string        // https://api.dev.loopzero.example
    PromPushGateway string        // For result metrics
}
```

### Observability During Tests

| Tool | Purpose |
|------|---------|
| Grafana — Platform Overview | Real-time semaphore |
| Grafana — Build Pipeline | Build latency + queue depth |
| Grafana — Per-Tenant Health | Identify degraded tenants |
| `loopzero_load_test_*` metrics | Test-specific counters (push via gateway) |
| PG `pg_stat_activity` | Connection + lock monitoring |
| `kubectl top` | Real-time resource pressure |

### Dashboards to Create
1. **Load Test Dashboard**: Deploys/min, success rate, latency by stage, error breakdown
2. **Saturation Dashboard**: PG pool %, lock wait time, build queue, ArgoCD sync queue

---

## Execution Sequence

```
Week 1:  L0 (baseline) + L1 (10 tenants) + load generator tool
Week 2:  L2 (50 tenants) + infra scaling
Week 3:  L3 (100 tenants) + per-project ArgoCD decision
Week 4:  L4 (500 tenants) + stress + PG failover under load
Week 5:  L4 continued + bottleneck fixes
Week 6:  L5 design (multi-cell architecture)
Week 8:  L5 implementation + 2-cell pilot
Week 12: L6 design (multi-region)
Month 6: L7 capacity planning
```

---

## Success Criteria Per Level

| Level | Tenants | Deploy Latency p99 | Failure Rate | Recovery Time |
|-------|---------|--------------------|--------------|--------------| 
| L1 | 10 | < 5min | 0% | — |
| L2 | 50 | < 5min | 0% | — |
| L3 | 100 | < 7min | < 0.5% | < 2min |
| L4 | 500 | < 10min | < 1% | < 2min |
| L5 | 1,000 | < 10min | < 1% | < 5min (per cell) |
| L6 | 10,000 | < 10min | < 0.5% | < 30s (region failover) |
| L7 | 500,000 | < 10min | < 0.1% | < 30s |

---

## Cost Projection

| Level | Infra Cost/mo | Revenue Potential |
|-------|-------------|-------------------|
| L1-L2 (current) | ~$350 | Starter: 10 users |
| L3 (100) | ~$800 | Pro: 100 × $29 = $2,900 |
| L4 (500) | ~$2,500 | Pro: 500 × $29 = $14,500 |
| L5 (1,000) | ~$6,000 | Mixed: ~$35,000 |
| L6 (10,000) | ~$40,000 | Mixed: ~$250,000 |
| L7 (500,000) | ~$1,800,000 | Mixed: ~$8,000,000 |

Unit economics become positive at ~50 Pro tenants (~$1,450/mo revenue vs ~$800 infra).
