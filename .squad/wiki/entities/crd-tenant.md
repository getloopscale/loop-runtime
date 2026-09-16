---
type: Entity
title: "Tenant (CustomResourceDefinition)"
description: "The Tenant CRD and the 7 supporting Go types it references, declared in operators/api/v1."
resource: "operators/api/v1"
tags:
  - crd
  - kubernetes
  - entity
  - tenant
sources:
  - id: crd-tenant
    resource: "operators/api/v1/tenant_types.go#L42"
    note: "Tenant"
  - id: f-gitopsspec
    resource: "operators/api/v1/tenant_types.go#L141"
    note: "GitOpsSpec"
  - id: f-previewspec
    resource: "operators/api/v1/tenant_types.go#L152"
    note: "PreviewSpec"
  - id: f-resourcelimits
    resource: "operators/api/v1/tenant_types.go#L161"
    note: "ResourceLimits"
  - id: f-tenantenvironmententry
    resource: "operators/api/v1/tenant_types.go#L98"
    note: "TenantEnvironmentEntry"
  - id: f-tenantlist
    resource: "operators/api/v1/tenant_types.go#L196"
    note: "TenantList"
  - id: f-tenantspec
    resource: "operators/api/v1/tenant_types.go#L51"
    note: "TenantSpec"
  - id: f-tenantstatus
    resource: "operators/api/v1/tenant_types.go#L177"
    note: "TenantStatus"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

`Tenant` is one of the **eight wired CustomResourceDefinitions** in `operators/api/v1`, a Go
module of its own so the API server can depend on the types without depending on the operators.
The family below is 8 Go structs: the root, its List wrapper, its Spec and Status, and
the supporting types they reference.[^crd-tenant]


`spec.cellID` defaults to `cell-0` and is a **mirror** of `tenants.cell_id` in Postgres, which is
the canonical placement source of truth. Reading the CR to decide where a tenant lives inverts
that and is explicitly wrong — see
[`/components/multi-cell-routing.md`](/components/multi-cell-routing.md).

`spec.environments` is the list the tenant operator reaps against: an ArgoCD Application labelled
with the tenant and **not** in `expectedApplicationNames` is deleted. That is why the Environment
reconciler removes its own `(project, environment)` tuple from this list before releasing its
finalizer — it does not delete the Application itself, because this `.spec` has one owner.


# Schema


## `GitOpsSpec`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/tenant_types.go:141`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `RepoName` | `string` | json:repoName; +kubebuilder:validation:MinLength=1 | `operators/api/v1/tenant_types.go:144` |
| `Branch` | `string` | json:branch; +kubebuilder:validation:MinLength=1 | `operators/api/v1/tenant_types.go:148` |


## `PreviewSpec`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/tenant_types.go:152`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `MaxEnvironments` | `int` | json:maxEnvironments | `operators/api/v1/tenant_types.go:154` |
| `TTLHours` | `int` | json:ttlHours | `operators/api/v1/tenant_types.go:157` |


## `ResourceLimits`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/tenant_types.go:161`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `MaxCPU` | `resource.Quantity` | json:maxCPU | `operators/api/v1/tenant_types.go:163` |
| `MaxMemory` | `resource.Quantity` | json:maxMemory | `operators/api/v1/tenant_types.go:166` |
| `MaxStorage` | `resource.Quantity` | json:maxStorage | `operators/api/v1/tenant_types.go:169` |
| `MaxBuildConcurrency` | `int` | json:maxBuildConcurrency; +kubebuilder:validation:Minimum=1 | `operators/api/v1/tenant_types.go:173` |


## `Tenant`

3 printcolumn markers Declared at `operators/api/v1/tenant_types.go:42`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `metav1.TypeMeta` | `metav1.TypeMeta` | json:,inline; embedded | `operators/api/v1/tenant_types.go:43` |
| `metav1.ObjectMeta` | `metav1.ObjectMeta` | json:metadata,omitempty; embedded | `operators/api/v1/tenant_types.go:44` |
| `Spec` | `TenantSpec` | json:spec,omitempty | `operators/api/v1/tenant_types.go:46` |
| `Status` | `TenantStatus` | json:status,omitempty | `operators/api/v1/tenant_types.go:47` |


## `TenantEnvironmentEntry`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/tenant_types.go:98`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Name` | `string` | json:name; +kubebuilder:validation:MinLength=1; +kubebuilder:validation:MaxLength=63; +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` | `operators/api/v1/tenant_types.go:106` |
| `Cluster` | `string` | json:cluster; +kubebuilder:validation:MinLength=1; +kubebuilder:validation:MaxLength=63; +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` | `operators/api/v1/tenant_types.go:113` |
| `Project` | `string` | json:project; +kubebuilder:validation:MinLength=1; +kubebuilder:validation:MaxLength=63; +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` | `operators/api/v1/tenant_types.go:137` |


## `TenantList`

kubebuilder List wrapper for the CRD root Declared at `operators/api/v1/tenant_types.go:196`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `metav1.TypeMeta` | `metav1.TypeMeta` | json:,inline; embedded | `operators/api/v1/tenant_types.go:197` |
| `metav1.ListMeta` | `metav1.ListMeta` | json:metadata,omitempty; embedded | `operators/api/v1/tenant_types.go:198` |
| `Items` | `[]Tenant` | json:items | `operators/api/v1/tenant_types.go:199` |


## `TenantSpec`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/tenant_types.go:51`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `DisplayName` | `string` | json:displayName; +kubebuilder:validation:MinLength=1; +kubebuilder:validation:MaxLength=128; +kubebuilder:validation:Pattern=`^[a-zA-Z0-9][a-zA-Z0-9 _.\-]*$` | `operators/api/v1/tenant_types.go:57` |
| `Plan` | `PlanTier` | json:plan | `operators/api/v1/tenant_types.go:60` |
| `Environments` | `[]TenantEnvironmentEntry` | json:environments; +kubebuilder:validation:MinItems=0 | `operators/api/v1/tenant_types.go:77` |
| `GitOps` | `GitOpsSpec` | json:gitops | `operators/api/v1/tenant_types.go:80` |
| `Preview` | `PreviewSpec` | json:preview | `operators/api/v1/tenant_types.go:83` |
| `Limits` | `ResourceLimits` | json:limits | `operators/api/v1/tenant_types.go:86` |
| `CellID` | `string` | json:cellID,omitempty; +kubebuilder:default="cell-0"; +kubebuilder:validation:MinLength=1 | `operators/api/v1/tenant_types.go:93` |


## `TenantStatus`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/tenant_types.go:177`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Phase` | `TenantPhase` | json:phase,omitempty | `operators/api/v1/tenant_types.go:179` |
| `GitOpsRepo` | `string` | json:gitopsRepo,omitempty | `operators/api/v1/tenant_types.go:182` |
| `Conditions` | `[]metav1.Condition` | json:conditions,omitempty | `operators/api/v1/tenant_types.go:187` |
| `ObservedGeneration` | `int64` | json:observedGeneration,omitempty | `operators/api/v1/tenant_types.go:190` |


# Relationships

- Declared in `operators/api/v1`, the CRD type module described in
  [`/components/operators.md`](/components/operators.md). That module has **efferent coupling 0
  and afferent coupling 11** — it depends on nothing in the workspace and eleven packages depend
  on it, which is the shape a pure type module should have.
- Reconciled by one of the four operator binaries; the reconciler persists `.status` with a merge
  patch off a base snapshot, never with `Status().Update`.
- Shipped to clusters by the `loopzero-crds` chart in
  [`/components/helm-charts.md`](/components/helm-charts.md).
- The other seven roots: [`ClusterCredential`](/entities/crd-clustercredential.md), [`Environment`](/entities/crd-environment.md), [`ExposedRoute`](/entities/crd-exposedroute.md), [`GitRepositoryBinding`](/entities/crd-gitrepositorybinding.md), [`PlatformBootstrap`](/entities/crd-platformbootstrap.md), [`PreviewEnvironment`](/entities/crd-previewenvironment.md), [`LoopZeroApplication`](/entities/crd-loopzeroapplication.md).

# Evidence

[^crd-tenant]: `operators/api/v1/tenant_types.go:42`
[^f-gitopsspec]: `operators/api/v1/tenant_types.go:141`
[^f-previewspec]: `operators/api/v1/tenant_types.go:152`
[^f-resourcelimits]: `operators/api/v1/tenant_types.go:161`
[^f-tenantenvironmententry]: `operators/api/v1/tenant_types.go:98`
[^f-tenantlist]: `operators/api/v1/tenant_types.go:196`
[^f-tenantspec]: `operators/api/v1/tenant_types.go:51`
[^f-tenantstatus]: `operators/api/v1/tenant_types.go:177`
