---
type: Entity
title: "PreviewEnvironment (CustomResourceDefinition)"
description: "The PreviewEnvironment CRD and the 3 supporting Go types it references, declared in operators/api/v1."
resource: "operators/api/v1"
tags:
  - crd
  - kubernetes
  - entity
  - previewenvironment
sources:
  - id: crd-previewenvironment
    resource: "operators/api/v1/preview_types.go#L32"
    note: "PreviewEnvironment"
  - id: f-previewenvironmentlist
    resource: "operators/api/v1/preview_types.go#L98"
    note: "PreviewEnvironmentList"
  - id: f-previewenvironmentspec
    resource: "operators/api/v1/preview_types.go#L42"
    note: "PreviewEnvironmentSpec"
  - id: f-previewenvironmentstatus
    resource: "operators/api/v1/preview_types.go#L81"
    note: "PreviewEnvironmentStatus"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

`PreviewEnvironment` is one of the **eight wired CustomResourceDefinitions** in `operators/api/v1`, a Go
module of its own so the API server can depend on the types without depending on the operators.
The family below is 4 Go structs: the root, its List wrapper, its Spec and Status, and
the supporting types they reference.[^crd-previewenvironment]


Backed by a single-slot source of truth in the `preview_slot` table rather than by counting CRs,
so a preview cannot be double-allocated. Its lifecycle has a state most do not —
`CleanupFailed`, reachable from both `Deleting` and `Expiring`, and leaving only by garbage
collection: [`/entities/state-machine-preview.md`](/entities/state-machine-preview.md).


# Schema


## `PreviewEnvironment`

3 printcolumn markers Declared at `operators/api/v1/preview_types.go:32`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `metav1.TypeMeta` | `metav1.TypeMeta` | json:,inline; embedded | `operators/api/v1/preview_types.go:33` |
| `metav1.ObjectMeta` | `metav1.ObjectMeta` | json:metadata,omitempty; embedded | `operators/api/v1/preview_types.go:34` |
| `Spec` | `PreviewEnvironmentSpec` | json:spec,omitempty | `operators/api/v1/preview_types.go:36` |
| `Status` | `PreviewEnvironmentStatus` | json:status,omitempty | `operators/api/v1/preview_types.go:37` |


## `PreviewEnvironmentList`

kubebuilder List wrapper for the CRD root Declared at `operators/api/v1/preview_types.go:98`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `metav1.TypeMeta` | `metav1.TypeMeta` | json:,inline; embedded | `operators/api/v1/preview_types.go:99` |
| `metav1.ListMeta` | `metav1.ListMeta` | json:metadata,omitempty; embedded | `operators/api/v1/preview_types.go:100` |
| `Items` | `[]PreviewEnvironment` | json:items | `operators/api/v1/preview_types.go:101` |


## `PreviewEnvironmentSpec`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/preview_types.go:42`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `TenantID` | `string` | json:tenantId; +kubebuilder:validation:MinLength=1 | `operators/api/v1/preview_types.go:45` |
| `ProjectID` | `string` | json:projectId; +kubebuilder:validation:MinLength=1 | `operators/api/v1/preview_types.go:49` |
| `CellID` | `string` | json:cellId,omitempty; +kubebuilder:validation:Optional | `operators/api/v1/preview_types.go:63` |
| `PRNumber` | `int` | json:prNumber; +kubebuilder:validation:Minimum=1 | `operators/api/v1/preview_types.go:67` |
| `Branch` | `string` | json:branch; +kubebuilder:validation:MinLength=1 | `operators/api/v1/preview_types.go:71` |
| `ExpiresAt` | `*metav1.Time` | json:expiresAt | `operators/api/v1/preview_types.go:74` |
| `ReleaseID` | `string` | json:releaseId | `operators/api/v1/preview_types.go:77` |


## `PreviewEnvironmentStatus`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/preview_types.go:81`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Phase` | `PreviewPhase` | json:phase,omitempty | `operators/api/v1/preview_types.go:83` |
| `URL` | `string` | json:url,omitempty | `operators/api/v1/preview_types.go:86` |
| `CreatedAt` | `*metav1.Time` | json:createdAt,omitempty | `operators/api/v1/preview_types.go:89` |
| `Conditions` | `[]metav1.Condition` | json:conditions,omitempty | `operators/api/v1/preview_types.go:92` |


# Relationships

- Declared in `operators/api/v1`, the CRD type module described in
  [`/components/operators.md`](/components/operators.md). That module has **efferent coupling 0
  and afferent coupling 11** — it depends on nothing in the workspace and eleven packages depend
  on it, which is the shape a pure type module should have.
- Reconciled by one of the four operator binaries; the reconciler persists `.status` with a merge
  patch off a base snapshot, never with `Status().Update`.
- Shipped to clusters by the `loopzero-crds` chart in
  [`/components/helm-charts.md`](/components/helm-charts.md).
- The other seven roots: [`ClusterCredential`](/entities/crd-clustercredential.md), [`Environment`](/entities/crd-environment.md), [`ExposedRoute`](/entities/crd-exposedroute.md), [`GitRepositoryBinding`](/entities/crd-gitrepositorybinding.md), [`PlatformBootstrap`](/entities/crd-platformbootstrap.md), [`Tenant`](/entities/crd-tenant.md), [`LoopZeroApplication`](/entities/crd-loopzeroapplication.md).

# Evidence

[^crd-previewenvironment]: `operators/api/v1/preview_types.go:32`
[^f-previewenvironmentlist]: `operators/api/v1/preview_types.go:98`
[^f-previewenvironmentspec]: `operators/api/v1/preview_types.go:42`
[^f-previewenvironmentstatus]: `operators/api/v1/preview_types.go:81`
