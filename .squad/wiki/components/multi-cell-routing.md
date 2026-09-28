---
type: Component
title: "Multi-cell placement and Kubernetes access"
description: "Assigns a tenant to a cell exactly once, then routes to it by pure lookup; plus the client-go adapters that reach each cell."
resource: "api/internal/placement"
tags:
  - multi-cell
  - placement
  - kubernetes
  - routing
sources:
  - id: mc-place
    resource: "api/internal/placement/dynamic_router.go#L1"
    note: "placement"
  - id: mc-cellmgr
    resource: "api/internal/cellmgr/client_manager.go#L3"
    note: "cellmgr"
  - id: mc-k8s
    resource: "api/internal/k8s/doc.go#L3"
    note: "k8s (plumbing)"
  - id: mc-ten
    resource: "api/internal/k8s/tenancy/tenant_cr_manager.go#L3"
    note: "k8s/tenancy"
  - id: mc-wl
    resource: "api/internal/k8s/workload/application_cr_manager.go#L3"
    note: "k8s/workload"
  - id: mc-ss
    resource: "api/internal/k8s/secretsync/secret_k8s_manager.go#L3"
    note: "k8s/secretsync"
  - id: mc-ad
    resource: "api/internal/adapters/adapters.go#L3"
    note: "adapters"
  - id: mc-health
    resource: "api/internal/health/prober.go#L3"
    note: "health"
  - id: mc-ports
    resource: "api/internal/ports/types.go#L3"
    note: "ports"
  - id: mc-read
    resource: "api/internal/placement/engine.go#L153"
    note: "CountByCell for the bin-packing decision, and GetCellForTenant for sticky routing afterwards. tenants.cell_id is the placement source of truth; the Tenant CR sp"
  - id: mc-health-orphan
    resource: "api/internal/health/prober.go#L3"
    note: "api/internal/health has no production caller, yet adapters.go names it as the required real implementation"
  - id: mc-cellmgr-orphan
    resource: "api/internal/cellmgr/client_manager.go#L3"
    note: "api/internal/cellmgr has zero importers, while CLAUDE.md lists it as a notable package"
  - id: mc-stale
    resource: "api/internal/adapters/adapters.go#L20"
    note: "adapters.go cites api/internal/gitea/gitops_pusher.go, a path that does not exist"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

Two responsibilities that are deliberately not the same one. **Placement** decides, once, which
runtime cell a tenant lands in. **Routing** looks that decision up, and contains no decision at
all — `DynamicRouter.Resolve(cellID)` is a hash lookup returning a kubeconfig, an ArgoCD
URL + token, and a base domain.[^mc-place]

`tenants.cell_id` in Postgres is the canonical source of truth. The `Tenant` CR's `spec.cellID`
is explicitly a **mirror** that must never be read for this purpose.[^mc-read]

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `api.placement` | `api/internal/placement` | package | infrastructure | `api/internal/placement/dynamic_router.go:1` | RoundRobinEngine (one-time sticky assignment) and DynamicRouter (pure hash lookup, zero decision), plus the canonical cell-id constants. Imports pgx and client-go. |
| `api.cellmgr` | `api/internal/cellmgr` | package | infrastructure | `api/internal/cellmgr/client_manager.go:3` | Lazy per-cell client-go client cache with an RWMutex double-check. |
| `api.k8s` | `api/internal/k8s` | package | infrastructure | `api/internal/k8s/doc.go:3` | Reduced to 3 files after the B-058 split. Its doc states the admission criterion explicitly — PLUMBING NAMES NO DOMAIN — and the gate `loopzero-ops quality package-admission` enforces both halves: no domain vocabulary here, and every one of the three new homes still populated. |
| `api.k8s.tenancy` | `api/internal/k8s/tenancy` | package | infrastructure | `api/internal/k8s/tenancy/tenant_cr_manager.go:3` | One of the three homes created by the B-058 split of api/internal/k8s. |
| `api.k8s.workload` | `api/internal/k8s/workload` | package | infrastructure | `api/internal/k8s/workload/application_cr_manager.go:3` | Second of the three B-058 homes. |
| `api.k8s.secretsync` | `api/internal/k8s/secretsync` | package | infrastructure | `api/internal/k8s/secretsync/secret_k8s_manager.go:3` | Third of the three B-058 homes. Deliberately the K8s half only — encryption lives in pkg/vault. |
| `api.adapters` | `api/internal/adapters` | package | application | `api/internal/adapters/adapters.go:3` | Bridge layer, and the only non-entrypoint package that imports both routes/ (builds, logs, promote, rollback, status) and db/ + services/ — which is what a bridge should look like. |
| `api.health` | `api/internal/health` | package | infrastructure | `api/internal/health/prober.go:3` | Outbound HTTP prober against deployed application URLs. |
| `api.ports` | `api/internal/ports` | package | domain | `api/internal/ports/types.go:3` | The DIP hub of the api module, and mechanically confirmed as one: go list shows `ports` importing NOTHING in-repo and nothing third-party, while 18 route packages and 7 service packages import it. |

## Measured size

| Component | loc | file count | afferent coupling | efferent coupling | instability | commits | commits last 90d |
|---|---|---|---|---|---|---|---|
| `api.placement` | 1998 | 10 | 5 | 1 | 0.1667 | 21 | 6 |
| `api.cellmgr` | 282 | 3 | 0 | 1 | 1 | 8 | 4 |
| `api.k8s.tenancy` | 1536 | 7 | 3 | 3 | 0.5 | 4 | 4 |
| `api.k8s.workload` | 851 | 4 | 2 | 1 | 0.3333 | 1 | 1 |
| `api.k8s.secretsync` | 668 | 3 | 1 | 2 | 0.6667 | 2 | 2 |
| `api.adapters` | 2444 | 16 | 2 | 12 | 0.8571 | 57 | 30 |
| `api.ports` | 1214 | 9 | 8 | 0 | 0 | 2 | 2 |

# Relationships

- `api.placement` **reads** `tenants` for `CountByCell` (the bin-packing input) and
  `GetCellForTenant` (sticky routing afterwards).[^mc-read]
- The decision itself — advisory lock, capacity check, hard failure when every cell is full —
  is [`/flows/tenant-cell-placement.md`](/flows/tenant-cell-placement.md).
- `api.k8s.*` **connect to** each cell's kube-apiserver with client-go / controller-runtime;
  `secretsync` writes `ExternalSecret` objects into workload namespaces, `tenancy` owns
  namespace-level objects, `workload` reads pod and deployment state.[^mc-k8s]
- `ClusterCredential` CRs are looked up by **`spec.cellID`, not by `metadata.name`** — see
  [`/entities/crd-clustercredential.md`](/entities/crd-clustercredential.md).
- The five clusters those credentials point at are
  [`/components/cluster-topology.md`](/components/cluster-topology.md).

## Two packages here have no caller

**`api.health`** provides real outbound HTTP probing of deployed application URLs, and has **no
production caller** — while `api/internal/adapters/adapters.go:22` names it as the required
real implementation.[^mc-health-orphan] The declaration and the wiring disagree, and the
declaration is the one that reads as authoritative.

**`api.cellmgr`** has zero importers, while `CLAUDE.md` lists it among the notable
packages.[^mc-cellmgr-orphan]

Separately, `adapters.go` cites `api/internal/gitea/gitops_pusher.go` — **a path that does not
exist**; the pusher lives at `api/internal/gitops/gitea_pusher.go`.[^mc-stale]

# Evidence

[^mc-place]: `api/internal/placement/dynamic_router.go:1`
[^mc-cellmgr]: `api/internal/cellmgr/client_manager.go:3`
[^mc-k8s]: `api/internal/k8s/doc.go:3`
[^mc-ten]: `api/internal/k8s/tenancy/tenant_cr_manager.go:3`
[^mc-wl]: `api/internal/k8s/workload/application_cr_manager.go:3`
[^mc-ss]: `api/internal/k8s/secretsync/secret_k8s_manager.go:3`
[^mc-ad]: `api/internal/adapters/adapters.go:3`
[^mc-health]: `api/internal/health/prober.go:3`
[^mc-ports]: `api/internal/ports/types.go:3`
[^mc-read]: `api/internal/placement/engine.go:153`
[^mc-health-orphan]: `api/internal/health/prober.go:3`
[^mc-cellmgr-orphan]: `api/internal/cellmgr/client_manager.go:3`
[^mc-stale]: `api/internal/adapters/adapters.go:20`
