# Cell-Based Architecture — Overview

> **ADR:** `docs/adr/0008-cell-based-architecture.md`
>
> **Target:** 50,000+ applications across 1,000+ tenants
>
> **Pattern:** Cell-based isolation (same as Heroku Private Spaces, Fly.io Cells, Salesforce Pods)
>
> **Principle:** Each Cell is a self-contained unit of execution. Cell failure is isolated. Scaling is linear.

---

## What is a Cell?

```
Cell = {
  1 Kubernetes workload cluster    (max ~5,000 apps)
  1 ArgoCD instance                (within CNOE-proven limits)
  1 Traefik ingress controller     (dedicated LoadBalancer IP)
  1 cert-manager                   (Let's Encrypt DNS-01)
  1 Prometheus                     (local metrics, federated to central)
  Promtail                         (ships logs to central Loki)
}
```

The Control Plane (mgmt cluster) is shared across all Cells:

```
Control Plane = {
  API (Go)           — cell-aware routing
  PostgreSQL         — partitioned by cell_id
  Gitea              — shared repos (1 per tenant, read by each Cell's ArgoCD)
  Argo Workflows     — shared build plane
  Prometheus central — federation from all Cells
  Grafana            — unified dashboards
  Loki               — centralized log storage
}
```

---

## Phase Map

```
Phase 0          Phase 1          Phase 2          Phase 3          Phase 4
Foundation       Cell-Ready       Multi-Cell       Automation       Optimization
─────────────────────────────────────────────────────────────────────────────────
cell_id field    Multi-cluster    2nd Cell live    Auto provision   ApplicationSets
PlacementEngine  client manager   Prometheus fed   Rebalancing      Auto-sync eval
CellRouter       Cell provisioner Loki multi-cell  Health monitor   ArgoCD tuning
Metrics          Cell-aware sync  Blast radius     Decommission     50k load test
                 DNS routing      isolation test   Cross-cell API   Cell versioning
                 ArgoCD per-cell  CF Worker        Runbooks
                 Health dashboard CLI --cells
```

---

## Phase Details

| Phase | Trigger | Duration | Key Deliverable | Risk Level |
|-------|---------|----------|-----------------|------------|
| **[0 — Foundation](cell-architecture-phase0.md)** | Now | 1 sprint | `cell_id` in tenant model, interfaces ready | LOW — zero behavioral change |
| **[1 — Cell-Ready](cell-architecture-phase1.md)** | 50+ tenants | 2 sprints | Adding Cell = config change, not code change | MEDIUM — multi-cluster client |
| **[2 — Multi-Cell](cell-architecture-phase2.md)** | 200+ tenants | 2 sprints | 2nd Cell live, blast radius proven | HIGH — first real multi-cell |
| **[3 — Automation](cell-architecture-phase3.md)** | 500+ tenants | 3 sprints | Auto provision, rebalancing, decommission | HIGH — tenant migration |
| **[4 — Optimization](cell-architecture-phase4.md)** | 1000+ tenants | 1 sprint | ApplicationSets, 50k load test | MEDIUM — ArgoCD optimization |

**Total: 9 sprints from Phase 0 to 50k validated**

---

## Scale Projection

| Tenants | Apps (est.) | Cells | ArgoCD Apps/Cell | Status |
|---------|-------------|-------|------------------|--------|
| 1-50 | 50-500 | 1 | 50-500 | Phase 0 (current) |
| 50-200 | 500-2,000 | 1-2 | 500-1,000 | Phase 1 |
| 200-500 | 2,000-5,000 | 2-5 | 1,000-2,500 | Phase 2 |
| 500-1,000 | 5,000-10,000 | 5-10 | 1,000-2,000 | Phase 3 |
| 1,000-5,000 | 10,000-50,000 | 10-20 | 2,500-5,000 | Phase 4 |
| 5,000+ | 50,000+ | 20+ | ~5,000 | Steady state |

---

## What Changes Per Component

| Component | Phase 0 | Phase 1 | Phase 2 | Phase 3 | Phase 4 |
|-----------|---------|---------|---------|---------|---------|
| **Tenant model** | +cell_id | — | — | +migrating state | — |
| **API** | passthrough | cell-aware routing | cross-cell logs | cross-cell queries | — |
| **Operators** | — | cell-aware ArgoCD | — | cell health ctrl | — |
| **ArgoCD** | 1 instance | cell-aware provisioner | 2 instances | N instances | ApplicationSets |
| **Clusters** | 1 workload | multi-cluster client | 2 workload | N workload | — |
| **Pulumi** | — | Cell provisioner | 2nd Cell | auto provision | Cell templates |
| **DNS/Routing** | — | CF Worker update | multi-cell routing | — | — |
| **Monitoring** | +cell metric | cell dashboard | Prometheus fed | cell health alerts | — |
| **Logs** | — | — | Loki multi-cell | — | — |
| **CLI** | — | — | +--cells flag | — | — |

## What NEVER Changes

| Component | Why |
|-----------|-----|
| **CLI UX** | `loopzero deploy` is transparent — Cell is internal implementation |
| **API contracts** | No new fields in `--json` output (cell_id is internal, not user-facing) |
| **Deploy flow** | build → manifest → gitops → ArgoCD → converge (same, per-Cell) |
| **State machines** | Same 7 machines, same transitions, same timeouts |
| **Advisory locks** | Same `pg_advisory_xact_lock` — tenant stays in 1 Cell, locks are local |
| **Gitea model** | Same repos, same structure — each Cell's ArgoCD reads the same repo |
| **Build pipeline** | Same Argo Workflows + BuildKit — shared, not per-Cell |
| **Exit codes** | 0/1/2/3/4/5 — permanent |

---

## Cost Model

| Cells | DOKS clusters | ArgoCD pods | Traefik pods | Est. monthly cost (DO) |
|-------|---------------|-------------|--------------|------------------------|
| 1 | 1 (current) | 3 | 2 | ~$200 (current) |
| 2 | 2 | 6 | 4 | ~$400 |
| 5 | 5 | 15 | 10 | ~$1,000 |
| 10 | 10 | 30 | 20 | ~$2,000 |
| 20 | 20 | 60 | 40 | ~$4,000 |

Each Cell adds ~$200/month in infrastructure (2 nodes + services). At 50k apps
with 20 Cells, the infrastructure cost is ~$4,000/month — amortized at $0.08/app/month.

---

## Key Decisions

| Decision | Rationale | ADR |
|----------|-----------|-----|
| **Keep ArgoCD** | Works within Cell limits (5k apps). CNOE-proven. Don't reinvent prune/health. | ADR-0008 |
| **Tenant sticky to Cell** | No cross-cell traffic. Advisory locks local. Simplest consistency model. | ADR-0008 |
| **Shared Gitea** | Each Cell's ArgoCD reads same Gitea repos. No repo duplication. | ADR-0008 |
| **Shared build plane** | Argo Workflows is stateless (per-job). No need for per-Cell build clusters. | ADR-0008 |
| **Prometheus federation** | Each Cell scrapes locally. Central aggregates. Proven pattern. | Phase 2 |
| **CF Worker routing** | Already exists for static delivery. Extend for Cell routing. | Phase 1 |

---

## Risk Summary

| Phase | Top Risk | Mitigation |
|-------|----------|------------|
| 0 | None (zero behavioral change) | — |
| 1 | Multi-cluster client bugs | envtest with 2 mock clusters |
| 2 | DNS routing errors during Cell-2 activation | Blue-green DNS cutover |
| 3 | Tenant migration data loss | Blue-green at Cell level, rollback capability |
| 4 | ApplicationSet SPOF | 1 AppSet per Cell limits blast radius |

---

## References

- [ADR-0008: Cell-Based Architecture](../adr/0008-cell-based-architecture.md)
- [MEET-2026-03-31-024: ArgoCD Scalability Meeting](../meetings/records/MEET-2026-03-31-024.md)
- [CNOE: Argo CD Benchmarking at 50k Apps](https://cnoe.io/blog/argo-cd-application-scalability)
- [Fly.io: Cell-Based Architecture](https://fly.io/docs/blueprints/cell-based/)
- [Kubernetes Large Cluster Limits](https://kubernetes.io/docs/setup/best-practices/cluster-large/)
- [Heroku: How Heroku Works](https://devcenter.heroku.com/articles/how-heroku-works)
