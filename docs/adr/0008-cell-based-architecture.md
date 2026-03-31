# ADR-0008: Cell-Based Architecture for 50k+ App Scale

**Status:** ACCEPTED
**Date:** 2026-03-31
**Supersedes:** ADR-0007 (ArgoCD Application Model) — subsumed by Cell model
**Origin:** MEET-2026-03-31-024 (ArgoCD scalability analysis + cross-validation evidence)
**Deciders:** Engineering Lead, Principal Architect, Distributed Systems, SRE, GitOps

---

## Context

### The Problem

Cross-validation with Epinio, Korifi, Heroku, and Fly.io, combined with ArgoCD benchmark
data (CNOE: 50k apps tested), revealed that LoopZero's current single-cluster architecture
cannot scale to 50,000+ applications. The question "should we keep ArgoCD?" was reframed
as the deeper question: "how do we partition 50k apps into manageable units?"

### Evidence Base (Real Data)

| Source | Finding | Link |
|--------|---------|------|
| **Kubernetes docs** | Max 150,000 pods per cluster, 5,000 nodes | [kubernetes.io](https://kubernetes.io/docs/setup/best-practices/cluster-large/) |
| **CNOE benchmark** | ArgoCD single shard saturates at ~30k apps on m5.2xlarge. 50k tested with 3 shards across 500 clusters. | [cnoe.io](https://cnoe.io/blog/argo-cd-application-scalability) |
| **GitHub #19113** | ApplicationSet controller is single-threaded. 250 AppSets + 5k apps = 5-10 min reconcile. Cannot scale horizontally. | [github.com](https://github.com/argoproj/argo-cd/discussions/19113) |
| **AWS EKS** | 100k nodes tested with 10M K8s objects, 32GB etcd (custom sharded). | [infoq.com](https://www.infoq.com/news/2025/09/aws-eks-kubernetes-ultrascale/) |
| **Heroku** | 1 Dyno Manager per region (Common Runtime). Private Spaces = dedicated Cell. | [heroku.com](https://devcenter.heroku.com/articles/dynos) |
| **Fly.io** | Explicitly cell-based architecture. Each Cell is self-sufficient. | [fly.io](https://fly.io/docs/blueprints/cell-based/) |
| **Korifi** | Zero ArgoCD. Controller-runtime direct apply. Limited by single etcd/cluster. | [github.com/cloudfoundry/korifi](https://github.com/cloudfoundry/korifi/blob/main/docs/architecture.md) |
| **Epinio** | Zero ArgoCD. Helm install direto. Single cluster. | Local codebase analysis |

### The Math

50,000 apps with 5 K8s resources each (Deployment, Service, ConfigMap, IngressRoute, Certificate)
= 250,000 K8s resource objects.

With 1-3 pods per app = 50k-150k pods. Kubernetes max per cluster = 150k pods.

**Conclusion: 50k apps requires multi-cluster. This is a physical constraint, not a design choice.**

### Industry Pattern

| PaaS | Scale Model | Cell Equivalent |
|------|------------|-----------------|
| **Heroku** | 1 Dyno Manager per region. Private Spaces = dedicated isolation unit. | Private Space = Cell |
| **Fly.io** | Explicitly cell-based. Each Cell has compute, networking, storage. | Cell = Cell |
| **Salesforce** | Pod architecture. Each Pod serves ~10k orgs with independent DB and app servers. | Pod = Cell |
| **Cloudflare** | Each PoP is independently functional. | PoP = Cell |
| **AWS** | Availability Zones provide failure isolation per Cell. | AZ = Cell |

---

## Decision

**Adopt Cell-Based Architecture as the scaling foundation for LoopZero.**

A **Cell** is a self-contained unit of execution:

```
Cell = {
  1 Kubernetes workload cluster (max ~5k apps)
  1 ArgoCD instance (within proven CNOE limits)
  1 Traefik ingress controller
  Cert-manager
  Prometheus (local, federated to central)
}
```

The **Control Plane** (management cluster) orchestrates N Cells:

```
Control Plane = {
  API (Go) — cell-aware routing
  PostgreSQL — partitioned by cell_id
  Gitea — shared, repos organized by tenant
  Argo Workflows — shared build plane
  Placement Engine — assigns tenants to Cells
  Cell Router — routes API calls to correct Cell
  Prometheus central — federation target
  Grafana — unified dashboards
}
```

### Architecture

```
┌──────────────────────────────────────────────────────────────┐
│  CONTROL PLANE (mgmt cluster)                                 │
│                                                                │
│  ┌─────────┐  ┌────────┐  ┌──────┐  ┌──────────────────────┐ │
│  │ LoopZero API │  │ Gitea  │  │  PG  │  │  Argo Workflows      │ │
│  │ (cell-   │  │(shared)│  │(part)│  │  (shared build)      │ │
│  │  aware)  │  │        │  │      │  │                      │ │
│  └────┬─────┘  └────────┘  └──────┘  └──────────────────────┘ │
│       │                                                        │
│  ┌────┴──────────────────┐                                    │
│  │ Placement Engine      │  Assigns tenants to Cells          │
│  │ Cell Router           │  Routes operations to correct Cell  │
│  └────┬──────────────────┘                                    │
├───────┼────────────────────────────────────────────────────────┤
│       │                                                        │
│  ┌────┴────┐  ┌─────────┐  ┌─────────┐       ┌─────────┐    │
│  │ CELL-1  │  │ CELL-2  │  │ CELL-3  │  ...  │ CELL-N  │    │
│  │ ≤5k apps│  │ ≤5k apps│  │ ≤5k apps│       │ ≤5k apps│    │
│  │         │  │         │  │         │       │         │    │
│  │ K8s     │  │ K8s     │  │ K8s     │       │ K8s     │    │
│  │ ArgoCD  │  │ ArgoCD  │  │ ArgoCD  │       │ ArgoCD  │    │
│  │ Traefik │  │ Traefik │  │ Traefik │       │ Traefik │    │
│  │ Prom    │  │ Prom    │  │ Prom    │       │ Prom    │    │
│  └─────────┘  └─────────┘  └─────────┘       └─────────┘    │
└──────────────────────────────────────────────────────────────┘
```

### Key Design Decisions

**1. Tenant Affinity:** A tenant is placed in exactly ONE Cell. All environments of a
tenant (staging, production) reside in the same Cell. This ensures:
- No cross-cell network traffic for tenant operations
- Advisory locks within a Cell are sufficient
- ArgoCD convergence is local

**2. Cell Sizing:** ~5,000 apps per Cell. Based on:
- K8s: 5k apps × 3 pods × 5 resources = 75k objects (well within 150k pod limit)
- ArgoCD: 5k Applications per shard is comfortable (CNOE tested 30k per shard)
- Operational: each Cell is independently operable

**3. ArgoCD Per Cell:** Each Cell has its own ArgoCD instance. This eliminates:
- ArgoCD sharding complexity (no need — each instance handles ~5k apps)
- Cross-cell blast radius (Cell-3 ArgoCD down does not affect Cell-1)
- ApplicationSet SPOF (each Cell can use ApplicationSets independently)

**4. Shared Build Plane:** Argo Workflows remains centralized. Build artifacts (images)
are pushed to a shared registry. Cells pull from the same registry.

**5. Shared Gitea:** One Gitea instance, repos organized per-tenant. Each Cell's ArgoCD
reads from the same Gitea repos. No repo duplication.

**6. PostgreSQL Partitioning:** Add `cell_id` column to key tables. Partition by cell_id
for query performance at scale.

### What Changes Per Phase

| Component | Phase 0 | Phase 1 | Phase 2 | Phase 3 |
|-----------|---------|---------|---------|---------|
| Tenant model | Add `cell_id` field | PlacementEngine assigns | Auto-placement | Rebalancing |
| API | cell_id passthrough | Cell-aware routing | Multi-cell queries | Federation |
| ArgoCD | 1 instance (current) | 1 instance | 2 instances | N instances |
| Workload clusters | 1 cluster | 1 cluster | 2 clusters | N clusters |
| Pulumi | Current 3-phase | Add Cell provisioner | Multi-cell | Auto-scaling |
| Ingress | 1 Traefik | 1 Traefik | DNS routing | Cloudflare Worker |
| Monitoring | Central | Central | Federation | Federation |

### What Does NOT Change

- **CLI** — `loopzero deploy` is transparent. Cell is internal.
- **API contracts** — No new fields in `--json` output.
- **Deploy flow** — build → manifest → gitops → ArgoCD → convergence (same, per-Cell)
- **State machines** — Same 7 machines, same transitions
- **Advisory locks** — Same `pg_advisory_xact_lock` per (tenant, project, env)
- **Gitea model** — Same repos, same structure
- **Build pipeline** — Same Argo Workflows + BuildKit

---

## Alternatives Considered

### A: ArgoCD Sharding (Single Cluster)

Shard the ArgoCD application controller into N replicas, each handling a subset of
Applications. CNOE tested this at 50k with 3 shards.

**Rejected because:**
- Does not solve the K8s cluster limit (150k pods). Multi-cluster is still needed.
- Sharding adds operational complexity within a single blast radius.
- If the shared etcd fails, ALL shards are down.
- Cells provide natural sharding boundaries without ArgoCD-specific configuration.

### B: Remove ArgoCD, Controller-Runtime Direct Apply

Replace ArgoCD with a custom `WorkloadReconciler` using server-side apply.

**Rejected because:**
- At 50k apps, multi-cluster is required regardless of convergence tool.
- ArgoCD within a Cell (5k apps) is within proven limits — no need to replace.
- Prune, health assessment, and drift detection are non-trivial to reimplement.
- The "Don't Reinvent the Wheel" principle applies: ArgoCD solves these problems.

### C: Flux CD

Replace ArgoCD with Flux. Lighter footprint, multi-tenancy native.

**Deferred:**
- Flux is a viable option within each Cell but requires ecosystem migration.
- Can be evaluated per-Cell in the future (each Cell can use a different syncer).
- Not worth the migration cost when ArgoCD works within Cell limits.

### D: No Cell Architecture (Single Mega-Cluster)

Scale a single K8s cluster to handle 50k apps using EKS ultra-scale (100k nodes).

**Rejected because:**
- AWS-specific (custom etcd sharding, Trainium-optimized).
- Single blast radius: any control plane failure affects ALL 50k apps.
- Violates the isolation principle: tenant failure in one part affects all.
- Not available on DigitalOcean (current provider).

---

## Consequences

### Positive

1. **Linear scaling:** Each Cell adds ~5k app capacity. 10 Cells = 50k. 20 Cells = 100k.
2. **Blast radius isolation:** Cell failure affects only tenants in that Cell.
3. **Independent upgrades:** Cells can be upgraded one at a time (canary per Cell).
4. **Provider agnostic:** Cells can be on different providers (DO Cell-1, AWS Cell-2).
5. **ArgoCD stays:** No need to replace — each Cell instance handles comfortable load.

### Negative

1. **Cross-cell queries are expensive:** "list all apps across all tenants" requires federation.
2. **Cell provisioning complexity:** Automating Cell creation adds IaC complexity.
3. **Tenant migration:** Moving a tenant between Cells requires coordinated state transfer.
4. **Cost:** Each Cell adds infrastructure cost (K8s cluster, ArgoCD pods, Traefik, Prometheus).

### Risks

| Severity | Risk | Mitigation |
|----------|------|------------|
| HIGH | Tenant migration between Cells is complex | Phase 3. Start with sticky placement. Migrate only when necessary. |
| MEDIUM | Cross-cell observability gaps | Prometheus federation + unified Grafana dashboards from Phase 2. |
| MEDIUM | Cell provisioning errors | Pulumi automation + envtest for Cell template. |
| LOW | Gitea becomes bottleneck at 1000+ repos | Evaluate Gitea clustering or per-Cell Gitea in Phase 3. |

---

## Implementation Phases

See `docs/plans/cell-architecture-overview.md` for the full overview and phase map.

| Phase | Plan | Trigger | Duration | Deliverables |
|-------|------|---------|----------|-------------|
| **0 — Foundation** | [phase0](../plans/cell-architecture-phase0.md) | Now | 1 sprint | `cell_id` in tenant model, PlacementEngine interface, CellRouter passthrough |
| **1 — Cell-Ready** | [phase1](../plans/cell-architecture-phase1.md) | 50+ tenants | 2 sprints | Multi-cluster client, Cell provisioner (Pulumi), ClusterCredential per Cell |
| **2 — Multi-Cell** | [phase2](../plans/cell-architecture-phase2.md) | 200+ tenants | 2 sprints | 2nd workload cluster, DNS routing, Prometheus federation, blast radius proof |
| **3 — Automation** | [phase3](../plans/cell-architecture-phase3.md) | 500+ tenants | 3 sprints | Auto Cell provisioning, tenant rebalancing, Cell health monitor, decommission |
| **4 — Optimization** | [phase4](../plans/cell-architecture-phase4.md) | 1000+ tenants | 1 sprint | ApplicationSets, ArgoCD tuning, Cell versioning, 50k load test |

---

## References

- MEET-2026-03-31-024: ArgoCD scalability meeting record
- MEET-2026-03-31-023: Cross-validation remediation
- ADR-0007: ArgoCD Application Model (subsumed by this ADR)
- [CNOE Argo CD Benchmarking](https://cnoe.io/blog/argo-cd-application-scalability)
- [Fly.io Cell-Based Architecture](https://fly.io/docs/blueprints/cell-based/)
- [Kubernetes Large Cluster Limits](https://kubernetes.io/docs/setup/best-practices/cluster-large/)
