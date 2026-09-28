---
type: Component
title: "Four kubebuilder operators"
description: "Tenant, App, Preview and Platform controllers — four binaries over a shared internal tree and one CRD API module."
resource: "operators"
tags:
  - kubernetes
  - operator
  - controller-runtime
  - kubebuilder
sources:
  - id: op-tenant
    resource: "operators/internal/tenant/tenant_controller.go#L1"
    note: "tenant (Tenant + Environment controllers)"
  - id: op-env
    resource: "operators/internal/environment/environment_controller.go#L1"
    note: "environment"
  - id: op-app
    resource: "operators/internal/app/application_controller.go#L1"
    note: "app (LoopZeroApplication + ExposedRoute)"
  - id: op-prev
    resource: "operators/internal/preview/preview_controller.go#L1"
    note: "preview"
  - id: op-plat
    resource: "operators/internal/platform/ca_fetcher.go#L1"
    note: "platform"
  - id: op-api
    resource: "operators/api/v1/groupversion_info.go#L1-L12"
    note: "loopzero.io/v1 CRD types"
  - id: op-cellrt
    resource: "operators/internal/cellrouting/runtime_client_factory.go#L1"
    note: "cellrouting"
  - id: op-loop
    resource: "operators/internal/reconcileloop/tracing.go#L1"
    note: "reconcileloop"
  - id: op-cmd-tenant
    resource: "operators/cmd/tenant-operator/main.go#L1"
    note: "loopzero-tenant-operator binary"
  - id: op-write
    resource: "operators/internal/environment/environment_rbac.go#L38"
    note: "the reconciler creates the namespace, Role, RoleBinding, NetworkPolicies and ResourceQuota on the CELL RUNTIME cluster — resolved per tenant through runtimeClie"
  - id: op-gitea
    resource: "operators/internal/gitea/provisioner.go#L1"
    note: "per-tenant organisation, repositories and tokens"
  - id: op-stale-design
    resource: "operators/internal/environment/environment_controller.go#L177"
    note: "12 Go source citations point at loopzero-design.md, a file deleted in commit 8cb41f660"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

Four separate binaries built from one module, each reconciling a different slice of the CRD
set: `tenant-operator`, `app-operator`, `preview-operator`, `platform-operator`.[^op-cmd-tenant]
They are kubebuilder / controller-runtime controllers running on the **control** cluster, acting
on the **runtime** cells through the credentials `cellrouting` resolves.[^op-cellrt]

`operators/internal/tenant` is the second-largest package in the repository at **9,613 lines
across 30 files**.

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `operators.api.v1` | `operators/api/v1` | package | domain | `operators/api/v1/groupversion_info.go:1-12` | 8 CRD Go types plus conditions, a tenant conversion and zz_generated.deepcopy.go. Verified on disk: application, clustercredential, environment, exposedroute, gitrepositorybinding, platformbootstrap, preview and tenant *_types.go — exactly the 8 the doc claims, with no AppSecret or ReconciliationReport file present. |
| `operators.app` | `operators/internal/app` | package | application | `operators/internal/app/application_controller.go:1` | 4 files / 3,452 lines: application controller, exposure controller, reaper and data-plane poller. |
| `operators.argocd` | `operators/internal/argocd` | package | infrastructure | `operators/internal/argocd/provisioner.go:1` | Provisioner interface + real implementation + circuit-breaker decorator — the same triple as gitea, harbor and minio. |
| `operators.cellrouting` | `operators/internal/cellrouting` | package | infrastructure | `operators/internal/cellrouting/runtime_client_factory.go:1` | Resolves how a controller reaches a cell's cluster. Split out of reconcilehelpers by B-061. Doc comment is written in Portuguese. |
| `operators.conditions` | `operators/internal/conditions` | package | domain | `operators/internal/conditions/types.go:1` | Adapter between pkg/conditions (apimachinery-free) and metav1.Condition, plus the operator-internal reason vocabulary. Kept as an alias shim after M6 moved the type vocabulary into operators/api/v1. |
| `operators.constants` | `operators/internal/constants` | package | foundation | `operators/internal/constants/finalizers.go:1` | The no-string-literals rule for operator code. go list confirms zero imports; imported by 8 operator packages. |
| `operators.environment` | `operators/internal/environment` | package | application | `operators/internal/environment/environment_controller.go:1` | The reference implementation of the single-status-writer invariant; its doc states the merge-patch-off-a-base-snapshot rule that CLAUDE.md calls the contract. |
| `operators.gitea` | `operators/internal/gitea` | package | infrastructure | `operators/internal/gitea/provisioner.go:1` | Per-tenant Gitea organisation, repositories and access tokens backing the GitOps source of truth. |
| `operators.harbor` | `operators/internal/harbor` | package | infrastructure | `operators/internal/harbor/client.go:1` | Stdlib-only Harbor v2 client; per-tenant projects and robot accounts. |
| `operators.httpclient` | `operators/internal/httpclient` | package | foundation | `operators/internal/httpclient/client.go:1` | One TLS configuration point for every operator-to-internal-service call (ARCH-005), so hardening is a single change. |
| `operators.metrics` | `operators/internal/metrics` | package | infrastructure | `operators/internal/metrics/metrics.go:1` | Prometheus collectors for the operators. Zero in-repo imports; imported by all four binaries. |
| `operators.minio` | `operators/internal/minio` | package | infrastructure | `operators/internal/minio/provisioner.go:1` | Per-tenant MinIO users with prefix-scoped policies, via the official madmin-go SDK for SigV4. |
| `operators.operatorboot` | `operators/internal/operatorboot` | package | foundation | `operators/internal/operatorboot/pprof.go:1` | 112 lines shared by all four binaries; what runs before the manager starts. Doc comment is written in Portuguese. |
| `operators.platform` | `operators/internal/platform` | package | application | `operators/internal/platform/ca_fetcher.go:1` | Holds kubeRootCAFetcher, which reads the live cluster CA on every reconcile and — per EC-13 — clones the restConfig with TLSClientConfig{Insecure:true} for that one GET, because using the stale CA to fetch the fresh CA cannot work after a rotation. |
| `operators.preview` | `operators/internal/preview` | package | application | `operators/internal/preview/preview_controller.go:1` | Namespace + NetworkPolicy + ResourceQuota + LimitRange + TTL expiry + cleanup retry. |
| `operators.reconcileloop` | `operators/internal/reconcileloop` | package | application | `operators/internal/reconcileloop/tracing.go:1` | Readiness summary, event predicates, finalizers, reconcile observability. Renamed from reconcilehelpers in B-061 after scoring 2/5 on cohesion; tenantpolicy, cellrouting and operatorboot were carved out of it. Doc comment is written in Portuguese. |
| `operators.tenant` | `operators/internal/tenant` | package | application | `operators/internal/tenant/tenant_controller.go:1` | 9 files / 9,613 lines — the largest operator package. Imports every provisioner (argocd, gitea, harbor, minio, vault). |
| `operators.tenantpolicy` | `operators/internal/tenantpolicy` | package | application | `operators/internal/tenantpolicy/limitrange.go:1` | The platform-imposed ceiling on a tenant namespace. Carved out of reconcilehelpers by B-061. Doc comment mixes English and Portuguese. |
| `operators.tracing` | `operators/internal/tracing` | package | infrastructure | `operators/internal/tracing/tracing.go:1` | OTel bootstrap for the operators. Zero in-repo imports; imported by all four binaries. |
| `operators.vault` | `operators/internal/vault` | package | domain | `operators/internal/vault/client.go:1` | 110 lines defining only the Transit subset the tenant controller needs. go list confirms it imports NOTHING — deliberately not pkg/vault, so the operator binary does not link the OpenBao SDK. |
| `operators.cmd.app` | `operators/cmd/app-operator` | service | entrypoint | `operators/cmd/app-operator/main.go:1` | Group B. Imports only internal/app, cellrouting, metrics, operatorboot, tracing and pkg/healthcheck — the 'zero external service dependencies' claim holds mechanically. |
| `operators.cmd.platform` | `operators/cmd/platform-operator` | service | entrypoint | `operators/cmd/platform-operator/main.go:1` | Group D, 145 lines. |
| `operators.cmd.preview` | `operators/cmd/preview-operator` | service | entrypoint | `operators/cmd/preview-operator/main.go:1` | Group C, 128 lines. |
| `operators.cmd.tenant` | `operators/cmd/tenant-operator` | service | entrypoint | `operators/cmd/tenant-operator/main.go:1` | Group A. go list confirms the doc: it is the ONLY operator binary importing argocd, gitea, harbor and minio, and the only one importing pkg/circuitbreaker. |

## Measured size

| Component | loc | file count | afferent coupling | efferent coupling | instability | commits | commits last 90d |
|---|---|---|---|---|---|---|---|
| `operators.api.v1` | 4170 | 22 | 11 | 0 | 0 | 38 | 10 |
| `operators.app` | 3452 | 10 | 1 | 10 | 0.9091 | 48 | 19 |
| `operators.argocd` | 1195 | 4 | 2 | 2 | 0.5 | 31 | 8 |
| `operators.cellrouting` | 426 | 3 | 6 | 1 | 0.1429 | 1 | 1 |
| `operators.conditions` | 1156 | 8 | 6 | 2 | 0.25 | 19 | 5 |
| `operators.constants` | 509 | 5 | 11 | 0 | 0 | 18 | 3 |
| `operators.environment` | 1219 | 7 | 1 | 9 | 0.9 | 9 | 9 |
| `operators.gitea` | 812 | 5 | 3 | 3 | 0.5 | 17 | 2 |
| `operators.harbor` | 1247 | 4 | 2 | 2 | 0.5 | 11 | 3 |
| `operators.httpclient` | 127 | 2 | 6 | 1 | 0.1429 | 5 | 2 |
| `operators.metrics` | 276 | 2 | 8 | 0 | 0 | 7 | 1 |
| `operators.minio` | 384 | 3 | 2 | 2 | 0.5 | 16 | 3 |
| `operators.operatorboot` | 112 | 2 | 4 | 0 | 0 | 1 | 1 |
| `operators.platform` | 4653 | 15 | 1 | 9 | 0.9 | 32 | 12 |
| `operators.preview` | 2065 | 7 | 1 | 9 | 0.9 | 15 | 13 |
| `operators.reconcileloop` | 838 | 7 | 5 | 2 | 0.2857 | 1 | 1 |
| `operators.tenant` | 9613 | 30 | 1 | 16 | 0.9412 | 27 | 25 |
| `operators.tenantpolicy` | 298 | 3 | 3 | 1 | 0.25 | 1 | 1 |
| `operators.tracing` | 137 | 1 | 4 | 0 | 0 | 4 | 2 |
| `operators.vault` | 110 | 2 | 1 | 0 | 0 | 5 | 4 |
| `operators.cmd.app` | 270 | 2 | 0 | 7 | 1 | 12 | 7 |
| `operators.cmd.platform` | 145 | 1 | 0 | 8 | 1 | 8 | 4 |
| `operators.cmd.preview` | 128 | 1 | 0 | 7 | 1 | 8 | 5 |
| `operators.cmd.tenant` | 309 | 1 | 0 | 14 | 1 | 12 | 7 |

The four `cmd` packages all show afferent coupling 0 and instability 1.0 — the composition-root
shape. `operators.api.v1` shows the inverse: efferent 0, afferent 11, instability 0.0, which is
what a pure type module should look like.

# The single-status-writer invariant

A CRD's `.status` has exactly **one** writer, and it persists with a merge patch taken off a
base snapshot captured right after the initial `Get` and *before* any status mutation:

```go
base := obj.DeepCopy()          // right after r.Get, BEFORE mutating .Status
// ... mutate obj.Status ...
r.Status().Patch(ctx, &obj, client.MergeFrom(base))
```

A merge patch carries no `resourceVersion`, so it never 409s against a concurrent spec or
metadata write by another controller. The refetch-then-restore `Status().Update` dance is a lost
update and is not used. Steps 6 and 16 of
[`/flows/environment-reconcile-loop.md`](/flows/environment-reconcile-loop.md) show the
invariant in place.[^op-write]

# Relationships

- Every operator **writes** to the Kubernetes API of a cell: the Environment reconciler alone
  creates the namespace, Role, RoleBinding, NetworkPolicies and ResourceQuota.[^op-write]
- `operators.gitea`, `operators.harbor`, `operators.minio` and `operators.vault` **connect to**
  the four stateful backends in
  [`/components/platform-datastores.md`](/components/platform-datastores.md) to provision
  per-tenant organisations, projects, users and transit keys.[^op-gitea]
- `operators.argocd` provisions AppProjects and Applications in
  [`/components/platform-orchestrators.md`](/components/platform-orchestrators.md).
- `operators.api.v1` is the CRD type module — the eight roots and their 45 supporting structs
  are documented per-CRD under `entities/`, starting at
  [`/entities/crd-tenant.md`](/entities/crd-tenant.md).
- Phase transitions go through [`/components/shared-go-libraries.md`](/components/shared-go-libraries.md)
  `pkg/statemachine`; direct status assignment is forbidden and an invalid transition panics.

# Where the code and the documents disagree

Twelve Go source citations across this tree point at `loopzero-design.md`, **a file deleted in
commit `8cb41f660`**.[^op-stale-design] The contract those comments defer to now lives in
`CLAUDE.md` — but a reader following the citation finds nothing, and the code gives no hint of
where it went.

# Evidence

[^op-tenant]: `operators/internal/tenant/tenant_controller.go:1`
[^op-env]: `operators/internal/environment/environment_controller.go:1`
[^op-app]: `operators/internal/app/application_controller.go:1`
[^op-prev]: `operators/internal/preview/preview_controller.go:1`
[^op-plat]: `operators/internal/platform/ca_fetcher.go:1`
[^op-api]: `operators/api/v1/groupversion_info.go:1-12`
[^op-cellrt]: `operators/internal/cellrouting/runtime_client_factory.go:1`
[^op-loop]: `operators/internal/reconcileloop/tracing.go:1`
[^op-cmd-tenant]: `operators/cmd/tenant-operator/main.go:1`
[^op-write]: `operators/internal/environment/environment_rbac.go:38`
[^op-gitea]: `operators/internal/gitea/provisioner.go:1`
[^op-stale-design]: `operators/internal/environment/environment_controller.go:177`
