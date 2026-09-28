---
type: Entity
title: "GitRepositoryBinding (CustomResourceDefinition)"
description: "The GitRepositoryBinding CRD and the 5 supporting Go types it references, declared in operators/api/v1."
resource: "operators/api/v1"
tags:
  - crd
  - kubernetes
  - entity
  - gitrepositorybinding
sources:
  - id: crd-gitrepositorybinding
    resource: "operators/api/v1/gitrepositorybinding_types.go#L21"
    note: "GitRepositoryBinding"
  - id: f-consumerspec
    resource: "operators/api/v1/gitrepositorybinding_types.go#L64"
    note: "ConsumerSpec"
  - id: f-credentialsecretref
    resource: "operators/api/v1/gitrepositorybinding_types.go#L49"
    note: "CredentialSecretRef"
  - id: f-gitrepositorybindinglist
    resource: "operators/api/v1/gitrepositorybinding_types.go#L93"
    note: "GitRepositoryBindingList"
  - id: f-gitrepositorybindingspec
    resource: "operators/api/v1/gitrepositorybinding_types.go#L30"
    note: "GitRepositoryBindingSpec"
  - id: f-gitrepositorybindingstatus
    resource: "operators/api/v1/gitrepositorybinding_types.go#L75"
    note: "GitRepositoryBindingStatus"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

`GitRepositoryBinding` is one of the **eight wired CustomResourceDefinitions** in `operators/api/v1`, a Go
module of its own so the API server can depend on the types without depending on the operators.
The family below is 6 Go structs: the root, its List wrapper, its Spec and Status, and
the supporting types they reference.[^crd-gitrepositorybinding]


Binds a tenant project to a Git repository. It is the CR behind the GitHub connect surface in
[`/entities/api-capabilities.md`](/entities/api-capabilities.md), and the auto-deploy path keys
its stored source bundle by **commit SHA** — `{tenant}/source/{commitSHA}.bundle` — so the SHA
from the webhook must match exactly.


# Schema


## `ConsumerSpec`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/gitrepositorybinding_types.go:64`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Type` | `string` | json:type; +kubebuilder:validation:Enum=argocd | `operators/api/v1/gitrepositorybinding_types.go:67` |
| `Namespace` | `string` | json:namespace; +kubebuilder:validation:MinLength=1 | `operators/api/v1/gitrepositorybinding_types.go:71` |


## `CredentialSecretRef`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/gitrepositorybinding_types.go:49`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Name` | `string` | json:name; +kubebuilder:validation:MinLength=1 | `operators/api/v1/gitrepositorybinding_types.go:52` |
| `Key` | `string` | json:key; +kubebuilder:validation:MinLength=1 | `operators/api/v1/gitrepositorybinding_types.go:56` |
| `Namespace` | `string` | json:namespace; +kubebuilder:validation:MinLength=1 | `operators/api/v1/gitrepositorybinding_types.go:60` |


## `GitRepositoryBinding`

3 printcolumn markers Declared at `operators/api/v1/gitrepositorybinding_types.go:21`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `metav1.TypeMeta` | `metav1.TypeMeta` | json:,inline; embedded | `operators/api/v1/gitrepositorybinding_types.go:22` |
| `metav1.ObjectMeta` | `metav1.ObjectMeta` | json:metadata,omitempty; embedded | `operators/api/v1/gitrepositorybinding_types.go:23` |
| `Spec` | `GitRepositoryBindingSpec` | json:spec,omitempty | `operators/api/v1/gitrepositorybinding_types.go:25` |
| `Status` | `GitRepositoryBindingStatus` | json:status,omitempty | `operators/api/v1/gitrepositorybinding_types.go:26` |


## `GitRepositoryBindingList`

kubebuilder List wrapper for the CRD root Declared at `operators/api/v1/gitrepositorybinding_types.go:93`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `metav1.TypeMeta` | `metav1.TypeMeta` | json:,inline; embedded | `operators/api/v1/gitrepositorybinding_types.go:94` |
| `metav1.ListMeta` | `metav1.ListMeta` | json:metadata,omitempty; embedded | `operators/api/v1/gitrepositorybinding_types.go:95` |
| `Items` | `[]GitRepositoryBinding` | json:items | `operators/api/v1/gitrepositorybinding_types.go:96` |


## `GitRepositoryBindingSpec`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/gitrepositorybinding_types.go:30`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `RepoURL` | `string` | json:repoURL; +kubebuilder:validation:MinLength=1; +kubebuilder:validation:MaxLength=2048; +kubebuilder:validation:Pattern=`^(https?://&#91;^\s]+\|ssh://git@&#91;^\s]+\|git@&#91;^\s:]+:&#91;^\s]+)$` | `operators/api/v1/gitrepositorybinding_types.go:39` |
| `CredentialSecretRef` | `CredentialSecretRef` | json:credentialSecretRef | `operators/api/v1/gitrepositorybinding_types.go:42` |
| `Consumer` | `ConsumerSpec` | json:consumer | `operators/api/v1/gitrepositorybinding_types.go:45` |


## `GitRepositoryBindingStatus`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/gitrepositorybinding_types.go:75`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `CredentialHash` | `string` | json:credentialHash,omitempty; +optional | `operators/api/v1/gitrepositorybinding_types.go:79` |
| `ObservedGeneration` | `int64` | json:observedGeneration,omitempty; +optional | `operators/api/v1/gitrepositorybinding_types.go:83` |
| `Conditions` | `[]metav1.Condition` | json:conditions,omitempty; +optional | `operators/api/v1/gitrepositorybinding_types.go:87` |


# Relationships

- Declared in `operators/api/v1`, the CRD type module described in
  [`/components/operators.md`](/components/operators.md). That module has **efferent coupling 0
  and afferent coupling 11** — it depends on nothing in the workspace and eleven packages depend
  on it, which is the shape a pure type module should have.
- Reconciled by one of the four operator binaries; the reconciler persists `.status` with a merge
  patch off a base snapshot, never with `Status().Update`.
- Shipped to clusters by the `loopzero-crds` chart in
  [`/components/helm-charts.md`](/components/helm-charts.md).
- The other seven roots: [`ClusterCredential`](/entities/crd-clustercredential.md), [`Environment`](/entities/crd-environment.md), [`ExposedRoute`](/entities/crd-exposedroute.md), [`PlatformBootstrap`](/entities/crd-platformbootstrap.md), [`PreviewEnvironment`](/entities/crd-previewenvironment.md), [`Tenant`](/entities/crd-tenant.md), [`LoopZeroApplication`](/entities/crd-loopzeroapplication.md).

# Evidence

[^crd-gitrepositorybinding]: `operators/api/v1/gitrepositorybinding_types.go:21`
[^f-consumerspec]: `operators/api/v1/gitrepositorybinding_types.go:64`
[^f-credentialsecretref]: `operators/api/v1/gitrepositorybinding_types.go:49`
[^f-gitrepositorybindinglist]: `operators/api/v1/gitrepositorybinding_types.go:93`
[^f-gitrepositorybindingspec]: `operators/api/v1/gitrepositorybinding_types.go:30`
[^f-gitrepositorybindingstatus]: `operators/api/v1/gitrepositorybinding_types.go:75`
