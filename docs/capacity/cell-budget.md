# Cell Capacity Budget

**Date:** 2026-04-10
**Source:** ADR-0008, L1 load test data, DigitalOcean resource limits
**Status:** Active — validated at L1 (10 tenants), projections for L2-L5

---

## Single Cell Budget

| Resource | Limit | Source | Monitoring Metric |
|----------|-------|--------|-------------------|
| **Tenants per cell** | 500 (conservative) | ADR-0008 capacity model | `loopzero_cell_tenant_count{cell_id}` |
| **Apps per cell** | ~5,000 | 500 tenants × ~10 apps avg | `loopzero_cell_tenant_count` × apps/tenant |
| **Concurrent builds** | 20 | Argo Workflows parallelism + PG pool budget | `loopzero_build_queue_depth` |
| **Concurrent builds per tenant** | 3 | `MAX_BUILDS_PER_TENANT` config | `loopzero_builds_active{tenant}` |
| **PG connections (API)** | 25 max / 5 min | `DB_MAX_CONNS` per pod | `loopzero_postgres_pool_total_conns` |
| **PG connections (total)** | 50 (2 API pods) + 25 (controller) = 75 | DO Managed PG max: 97 | `pg_stat_activity` |
| **ArgoCD applications** | 200 | 8GB RAM limit, ~40MB per 100 apps | ArgoCD metrics |
| **Runtime pods** | ~1,500 | 500 tenants × 3 pods avg (2-node medium) | `kube_pod_status_phase` |
| **Build queue timeout** | 5 min | `BUILD_QUEUE_TIMEOUT` config | `loopzero_build_duration_seconds` |
| **Build running timeout** | 30 min | `BUILD_RUNNING_TIMEOUT` config | `loopzero_build_duration_seconds` |

## Saturation Thresholds

| Resource | Yellow (70%) | Red (90%) | Action |
|----------|-------------|-----------|--------|
| PG pool | 53 connections | 68 connections | Red → PG circuit breaker rejects mutations (503) |
| Build queue | 14 concurrent | 18 concurrent | Red → admission gate rejects new builds (429) |
| ArgoCD apps | 140 apps | 180 apps | Red → defer new tenant onboarding |
| Runtime pods | ~1,050 | ~1,350 | Red → trigger cell provisioning |
| Mgmt memory | 70% node limit | 90% node limit | Red → scale mgmt node class |

## Infrastructure Sizing per Cell

### Minimum (L2 — 50 tenants)

| Component | Spec | Cost/mo |
|-----------|------|---------|
| Runtime cluster | 2× s-2vcpu-4gb ($24/mo each) | $48 |
| Build cluster | 2× s-2vcpu-4gb, autoscale→5 | $48-$120 |
| Mgmt cluster | shared (existing) | $0 |
| PG | shared (existing DO Managed) | $0 |
| ArgoCD | shared on mgmt (existing) | $0 |
| **Total delta** | | **~$150** |

### Standard (L3 — 100 tenants)

| Component | Spec | Cost/mo |
|-----------|------|---------|
| Runtime cluster | 2× s-4vcpu-8gb ($48/mo each) | $96 |
| Build cluster | 3× s-2vcpu-4gb, autoscale→5 | $72-$120 |
| Mgmt cluster | upgrade to s-4vcpu-8gb nodes | $96 |
| PG | upgrade to larger tier if needed | ~$50 |
| **Total delta** | | **~$450** |

### Full Cell (L4 — 500 tenants, 2 cells)

| Component | Spec | Cost/mo |
|-----------|------|---------|
| Per-cell runtime | 3× s-4vcpu-8gb | $144 |
| Per-cell ArgoCD | dedicated instance (8GB) | included in mgmt |
| Shared PG | DO Managed PG-8 (16GB, 200 conns) | $150 |
| **Per-cell cost** | | **~$300** |
| **Total (2 cells)** | | **~$2,500** |

## Admission Control Rules

1. **Cell-level gate**: Max 20 concurrent builds per cell. Rejection: HTTP 429 + `Retry-After: 5`.
2. **Tenant-level gate**: Max 3 concurrent builds per tenant. Prevents noisy neighbor.
3. **PG circuit breaker**: At 90% pool utilization, reject mutations (POST/PUT/DELETE) with HTTP 503. Reads (GET) continue.
4. **Queue depth alert**: `loopzero_build_queue_depth > 15` for 5 minutes → PagerDuty.

## Graceful Degradation at 110% Budget

At 110% of cell capacity (e.g., 22 concurrent builds, 55 of 50 PG connections):

| Symptom | Expected Behavior | Recovery |
|---------|-------------------|----------|
| Build submission | 429 with queue position | Automatic drain as builds complete |
| PG pool full | 503 for mutations, reads OK | Pool returns as transactions commit |
| ArgoCD sync delay | Sync p99 increases to 300s | Self-resolving as queue drains |
| API latency | p99 increases to 2s | Automatic as load decreases |

**Critical guarantee**: No data corruption. No process crashes. No lost builds. System slows but does not break.

## Scaling Trigger

Provision a new cell when ANY of these conditions persist for 30 minutes:

1. `loopzero_cell_tenant_count{cell_id} > 400` (80% of budget)
2. `loopzero_build_queue_depth > 15` sustained
3. PG pool > 80% sustained
4. Runtime pod count > 1,200

## References

- [ADR-0008: Cell-Based Architecture](../meetings/adr/0008-cell-based-architecture.md)
- [Load Test Plan](../plans/load-test-plan.md)
- [Roadmap Item 04](../roadmap/04-single-cell-hardening/roadmap.md)
- [Roadmap Item 07](../roadmap/07-backpressure-and-admission/roadmap.md)
