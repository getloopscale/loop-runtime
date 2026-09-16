---
type: Entity
title: "Environment (CustomResourceDefinition)"
description: "The Environment CRD and the 7 supporting Go types it references, declared in operators/api/v1."
resource: "operators/api/v1"
tags:
  - crd
  - kubernetes
  - entity
  - environment
sources:
  - id: crd-environment
    resource: "operators/api/v1/environment_types.go#L31"
    note: "Environment"
  - id: f-environmentcrspec
    resource: "operators/api/v1/environment_types.go#L43"
    note: "EnvironmentCRSpec"
  - id: f-environmentcrstatus
    resource: "operators/api/v1/environment_types.go#L115"
    note: "EnvironmentCRStatus"
  - id: f-environmentlist
    resource: "operators/api/v1/environment_types.go#L133"
    note: "EnvironmentList"
  - id: f-namespacespec
    resource: "operators/api/v1/environment_types.go#L89"
    note: "NamespaceSpec"
  - id: f-networkpolicyconfig
    resource: "operators/api/v1/environment_types.go#L108"
    note: "NetworkPolicyConfig"
  - id: f-resourcequotaspec
    resource: "operators/api/v1/environment_types.go#L96"
    note: "ResourceQuotaSpec"
  - id: f-tenantref
    resource: "operators/api/v1/environment_types.go#L82"
    note: "TenantRef"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

`Environment` is one of the **eight wired CustomResourceDefinitions** in `operators/api/v1`, a Go
module of its own so the API server can depend on the types without depending on the operators.
The family below is 8 Go structs: the root, its List wrapper, its Spec and Status, and
the supporting types they reference.[^crd-environment]


The Environment reconciler is the worked example of the single-status-writer invariant, traced
step by step in [`/flows/environment-reconcile-loop.md`](/flows/environment-reconcile-loop.md).
Its status carries four conditions — namespace, RBAC, network policy, quota — and the phase only
reaches `Active` when all four are True.

`spec.resourceQuota` and `spec.networkPolicy` are optional; when absent, those reconcile steps are
skipped rather than defaulted.


# Schema


## `Environment`

3 printcolumn markers Declared at `operators/api/v1/environment_types.go:31`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `metav1.TypeMeta` | `metav1.TypeMeta` | json:,inline; embedded | `operators/api/v1/environment_types.go:32` |
| `metav1.ObjectMeta` | `metav1.ObjectMeta` | json:metadata,omitempty; embedded | `operators/api/v1/environment_types.go:33` |
| `Spec` | `EnvironmentCRSpec` | json:spec,omitempty | `operators/api/v1/environment_types.go:35` |
| `Status` | `EnvironmentCRStatus` | json:status,omitempty | `operators/api/v1/environment_types.go:36` |


## `EnvironmentCRSpec`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/environment_types.go:43`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `TenantRef` | `TenantRef` | json:tenantRef | `operators/api/v1/environment_types.go:45` |
| `Environment` | `string` | json:environment; +kubebuilder:validation:MinLength=1 | `operators/api/v1/environment_types.go:49` |
| `Project` | `string` | json:project,omitempty; +kubebuilder:validation:Optional; +kubebuilder:validation:MaxLength=63; +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$` | `operators/api/v1/environment_types.go:65` |
| `Namespace` | `NamespaceSpec` | json:namespace | `operators/api/v1/environment_types.go:68` |
| `ResourceQuota` | `*ResourceQuotaSpec` | json:resourceQuota,omitempty; +optional | `operators/api/v1/environment_types.go:72` |
| `NetworkPolicy` | `NetworkPolicyConfig` | json:networkPolicy | `operators/api/v1/environment_types.go:75` |
| `Plan` | `PlanTier` | json:plan | `operators/api/v1/environment_types.go:78` |


## `EnvironmentCRStatus`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/environment_types.go:115`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Phase` | `EnvironmentPhase` | json:phase,omitempty | `operators/api/v1/environment_types.go:117` |
| `Namespace` | `string` | json:namespace,omitempty | `operators/api/v1/environment_types.go:120` |
| `ObservedGeneration` | `int64` | json:observedGeneration,omitempty | `operators/api/v1/environment_types.go:123` |
| `Conditions` | `[]metav1.Condition` | json:conditions,omitempty | `operators/api/v1/environment_types.go:127` |


## `EnvironmentList`

kubebuilder List wrapper for the CRD root Declared at `operators/api/v1/environment_types.go:133`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `metav1.TypeMeta` | `metav1.TypeMeta` | json:,inline; embedded | `operators/api/v1/environment_types.go:134` |
| `metav1.ListMeta` | `metav1.ListMeta` | json:metadata,omitempty; embedded | `operators/api/v1/environment_types.go:135` |
| `Items` | `[]Environment` | json:items | `operators/api/v1/environment_types.go:136` |


## `NamespaceSpec`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/environment_types.go:89`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Name` | `string` | json:name; +kubebuilder:validation:MinLength=1 | `operators/api/v1/environment_types.go:92` |


## `NetworkPolicyConfig`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/environment_types.go:108`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Isolate` | `bool` | json:isolate; +kubebuilder:default=true | `operators/api/v1/environment_types.go:111` |


## `ResourceQuotaSpec`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/environment_types.go:96`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `CPU` | `string` | json:cpu | `operators/api/v1/environment_types.go:98` |
| `Memory` | `string` | json:memory | `operators/api/v1/environment_types.go:101` |
| `Storage` | `string` | json:storage | `operators/api/v1/environment_types.go:104` |


## `TenantRef`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/environment_types.go:82`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Name` | `string` | json:name; +kubebuilder:validation:MinLength=1 | `operators/api/v1/environment_types.go:85` |


# Relationships

- Declared in `operators/api/v1`, the CRD type module described in
  [`/components/operators.md`](/components/operators.md). That module has **efferent coupling 0
  and afferent coupling 11** — it depends on nothing in the workspace and eleven packages depend
  on it, which is the shape a pure type module should have.
- Reconciled by one of the four operator binaries; the reconciler persists `.status` with a merge
  patch off a base snapshot, never with `Status().Update`.
- Shipped to clusters by the `loopzero-crds` chart in
  [`/components/helm-charts.md`](/components/helm-charts.md).
- The other seven roots: [`ClusterCredential`](/entities/crd-clustercredential.md), [`ExposedRoute`](/entities/crd-exposedroute.md), [`GitRepositoryBinding`](/entities/crd-gitrepositorybinding.md), [`PlatformBootstrap`](/entities/crd-platformbootstrap.md), [`PreviewEnvironment`](/entities/crd-previewenvironment.md), [`Tenant`](/entities/crd-tenant.md), [`LoopZeroApplication`](/entities/crd-loopzeroapplication.md).

# Evidence

[^crd-environment]: `operators/api/v1/environment_types.go:31`
[^f-environmentcrspec]: `operators/api/v1/environment_types.go:43`
[^f-environmentcrstatus]: `operators/api/v1/environment_types.go:115`
[^f-environmentlist]: `operators/api/v1/environment_types.go:133`
[^f-namespacespec]: `operators/api/v1/environment_types.go:89`
[^f-networkpolicyconfig]: `operators/api/v1/environment_types.go:108`
[^f-resourcequotaspec]: `operators/api/v1/environment_types.go:96`
[^f-tenantref]: `operators/api/v1/environment_types.go:82`
