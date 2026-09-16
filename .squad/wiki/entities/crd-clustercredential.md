---
type: Entity
title: "ClusterCredential (CustomResourceDefinition)"
description: "The ClusterCredential CRD and the 6 supporting Go types it references, declared in operators/api/v1."
resource: "operators/api/v1"
tags:
  - crd
  - kubernetes
  - entity
  - clustercredential
sources:
  - id: crd-clustercredential
    resource: "operators/api/v1/clustercredential_types.go#L19"
    note: "ClusterCredential"
  - id: f-clustercredentiallist
    resource: "operators/api/v1/clustercredential_types.go#L118"
    note: "ClusterCredentialList"
  - id: f-clustercredentialspec
    resource: "operators/api/v1/clustercredential_types.go#L28"
    note: "ClusterCredentialSpec"
  - id: f-clustercredentialstatus
    resource: "operators/api/v1/clustercredential_types.go#L77"
    note: "ClusterCredentialStatus"
  - id: f-clustertlsconfig
    resource: "operators/api/v1/clustercredential_types.go#L68"
    note: "ClusterTLSConfig"
  - id: f-credentialscope
    resource: "operators/api/v1/clustercredential_types.go#L59"
    note: "CredentialScope"
  - id: f-secretreference
    resource: "operators/api/v1/clustercredential_types.go#L128"
    note: "SecretReference"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

`ClusterCredential` is one of the **eight wired CustomResourceDefinitions** in `operators/api/v1`, a Go
module of its own so the API server can depend on the types without depending on the operators.
The family below is 7 Go structs: the root, its List wrapper, its Spec and Status, and
the supporting types they reference.[^crd-clustercredential]


**Looked up by `spec.cellID`, never by `metadata.name`.** A CR named `workload` carrying
`spec.cellID=cell-0` is valid and resolves correctly; assuming the two agree is a documented
mistake.

`spec.apiServer` is pattern-constrained to `^https?://` and `spec.cellID` to `^cell-[a-z0-9-]+$`.

The TLS block is where the live-versus-snapshot rule bites: the platform operator fetches
`default/kube-root-ca.crt` **live on every reconcile** rather than snapshotting it at bootstrap,
because a snapshot goes stale on CA rotation. The fetcher must connect with `Insecure: true` —
using the stale CA to fetch its own replacement fails the TLS handshake before the fetch runs.


# Schema


## `ClusterCredential`

3 printcolumn markers Declared at `operators/api/v1/clustercredential_types.go:19`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `metav1.TypeMeta` | `metav1.TypeMeta` | json:,inline; embedded | `operators/api/v1/clustercredential_types.go:20` |
| `metav1.ObjectMeta` | `metav1.ObjectMeta` | json:metadata,omitempty; embedded | `operators/api/v1/clustercredential_types.go:21` |
| `Spec` | `ClusterCredentialSpec` | json:spec,omitempty | `operators/api/v1/clustercredential_types.go:23` |
| `Status` | `ClusterCredentialStatus` | json:status,omitempty | `operators/api/v1/clustercredential_types.go:24` |


## `ClusterCredentialList`

kubebuilder List wrapper for the CRD root Declared at `operators/api/v1/clustercredential_types.go:118`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `metav1.TypeMeta` | `metav1.TypeMeta` | json:,inline; embedded | `operators/api/v1/clustercredential_types.go:119` |
| `metav1.ListMeta` | `metav1.ListMeta` | json:metadata,omitempty; embedded | `operators/api/v1/clustercredential_types.go:120` |
| `Items` | `[]ClusterCredential` | json:items | `operators/api/v1/clustercredential_types.go:121` |


## `ClusterCredentialSpec`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/clustercredential_types.go:28`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `ClusterName` | `string` | json:clusterName; +kubebuilder:validation:MinLength=1 | `operators/api/v1/clustercredential_types.go:31` |
| `APIServer` | `string` | json:apiServer; +kubebuilder:validation:MinLength=1; +kubebuilder:validation:Pattern=`^https?://` | `operators/api/v1/clustercredential_types.go:37` |
| `SecretRef` | `SecretReference` | json:secretRef | `operators/api/v1/clustercredential_types.go:40` |
| `Scope` | `CredentialScope` | json:scope | `operators/api/v1/clustercredential_types.go:43` |
| `TLS` | `ClusterTLSConfig` | json:tls,omitempty; +optional | `operators/api/v1/clustercredential_types.go:47` |
| `CellID` | `string` | json:cellID,omitempty; +kubebuilder:validation:MinLength=1; +kubebuilder:validation:Pattern=`^cell-[a-z0-9-]+$`; +kubebuilder:default="cell-0" | `operators/api/v1/clustercredential_types.go:55` |


## `ClusterCredentialStatus`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/clustercredential_types.go:77`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Connected` | `bool` | json:connected; +optional deliberately, and the JSON tag deliberately keeps no omitempty.; +optional; +kubebuilder:default=false | `operators/api/v1/clustercredential_types.go:101` |
| `LastCheckedAt` | `*metav1.Time` | json:lastCheckedAt,omitempty | `operators/api/v1/clustercredential_types.go:104` |
| `ObservedGeneration` | `int64` | json:observedGeneration,omitempty; +optional | `operators/api/v1/clustercredential_types.go:109` |
| `Conditions` | `[]metav1.Condition` | json:conditions,omitempty | `operators/api/v1/clustercredential_types.go:112` |


## `ClusterTLSConfig`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/clustercredential_types.go:68`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Insecure` | `bool` | json:insecure,omitempty; +kubebuilder:default=false; +optional | `operators/api/v1/clustercredential_types.go:73` |


## `CredentialScope`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/clustercredential_types.go:59`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `ArgoCD` | `bool` | json:argocd | `operators/api/v1/clustercredential_types.go:61` |
| `Operators` | `bool` | json:operators | `operators/api/v1/clustercredential_types.go:64` |


## `SecretReference`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/clustercredential_types.go:128`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Name` | `string` | json:name | `operators/api/v1/clustercredential_types.go:130` |
| `Namespace` | `string` | json:namespace | `operators/api/v1/clustercredential_types.go:133` |


# Relationships

- Declared in `operators/api/v1`, the CRD type module described in
  [`/components/operators.md`](/components/operators.md). That module has **efferent coupling 0
  and afferent coupling 11** — it depends on nothing in the workspace and eleven packages depend
  on it, which is the shape a pure type module should have.
- Reconciled by one of the four operator binaries; the reconciler persists `.status` with a merge
  patch off a base snapshot, never with `Status().Update`.
- Shipped to clusters by the `loopzero-crds` chart in
  [`/components/helm-charts.md`](/components/helm-charts.md).
- The other seven roots: [`Environment`](/entities/crd-environment.md), [`ExposedRoute`](/entities/crd-exposedroute.md), [`GitRepositoryBinding`](/entities/crd-gitrepositorybinding.md), [`PlatformBootstrap`](/entities/crd-platformbootstrap.md), [`PreviewEnvironment`](/entities/crd-previewenvironment.md), [`Tenant`](/entities/crd-tenant.md), [`LoopZeroApplication`](/entities/crd-loopzeroapplication.md).

# Evidence

[^crd-clustercredential]: `operators/api/v1/clustercredential_types.go:19`
[^f-clustercredentiallist]: `operators/api/v1/clustercredential_types.go:118`
[^f-clustercredentialspec]: `operators/api/v1/clustercredential_types.go:28`
[^f-clustercredentialstatus]: `operators/api/v1/clustercredential_types.go:77`
[^f-clustertlsconfig]: `operators/api/v1/clustercredential_types.go:68`
[^f-credentialscope]: `operators/api/v1/clustercredential_types.go:59`
[^f-secretreference]: `operators/api/v1/clustercredential_types.go:128`
