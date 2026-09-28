---
type: Entity
title: "LoopZeroApplication (CustomResourceDefinition)"
description: "The LoopZeroApplication CRD and the 3 supporting Go types it references, declared in operators/api/v1."
resource: "operators/api/v1"
tags:
  - crd
  - kubernetes
  - entity
  - loopzeroapplication
sources:
  - id: crd-loopzeroapplication
    resource: "operators/api/v1/application_types.go#L39"
    note: "LoopZeroApplication"
  - id: f-loopzeroapplicationlist
    resource: "operators/api/v1/application_types.go#L109"
    note: "LoopZeroApplicationList"
  - id: f-loopzeroapplicationspec
    resource: "operators/api/v1/application_types.go#L48"
    note: "LoopZeroApplicationSpec"
  - id: f-loopzeroapplicationstatus
    resource: "operators/api/v1/application_types.go#L85"
    note: "LoopZeroApplicationStatus"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

`LoopZeroApplication` is one of the **eight wired CustomResourceDefinitions** in `operators/api/v1`, a Go
module of its own so the API server can depend on the types without depending on the operators.
The family below is 4 Go structs: the root, its List wrapper, its Spec and Status, and
the supporting types they reference.[^crd-loopzeroapplication]


The workload primitive is `Deployment` **or** `Rollout`, depending on whether progressive
delivery is on. Any convergence loop or health checker that filters on `Kind != "Deployment"`
alone leaves every Rollout-backed release `PENDING` forever — a real regression the repository
records.

Its lifecycle is [`/entities/state-machine-application.md`](/entities/state-machine-application.md),
the only machine in the system whose terminal state is disputed by its own doc comment.


# Schema


## `LoopZeroApplication`

3 printcolumn markers Declared at `operators/api/v1/application_types.go:39`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `metav1.TypeMeta` | `metav1.TypeMeta` | json:,inline; embedded | `operators/api/v1/application_types.go:40` |
| `metav1.ObjectMeta` | `metav1.ObjectMeta` | json:metadata,omitempty; embedded | `operators/api/v1/application_types.go:41` |
| `Spec` | `LoopZeroApplicationSpec` | json:spec,omitempty | `operators/api/v1/application_types.go:43` |
| `Status` | `LoopZeroApplicationStatus` | json:status,omitempty | `operators/api/v1/application_types.go:44` |


## `LoopZeroApplicationList`

kubebuilder List wrapper for the CRD root Declared at `operators/api/v1/application_types.go:109`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `metav1.TypeMeta` | `metav1.TypeMeta` | json:,inline; embedded | `operators/api/v1/application_types.go:110` |
| `metav1.ListMeta` | `metav1.ListMeta` | json:metadata,omitempty; embedded | `operators/api/v1/application_types.go:111` |
| `Items` | `[]LoopZeroApplication` | json:items | `operators/api/v1/application_types.go:112` |


## `LoopZeroApplicationSpec`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/application_types.go:48`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `TenantRef` | `string` | json:tenantRef; +kubebuilder:validation:MinLength=1 | `operators/api/v1/application_types.go:51` |
| `Project` | `string` | json:project; +kubebuilder:validation:MinLength=1 | `operators/api/v1/application_types.go:55` |
| `Environment` | `string` | json:environment; +kubebuilder:validation:MinLength=1 | `operators/api/v1/application_types.go:59` |
| `ServiceName` | `string` | json:serviceName; +kubebuilder:validation:MinLength=1 | `operators/api/v1/application_types.go:63` |
| `Hostname` | `string` | json:hostname,omitempty; +optional | `operators/api/v1/application_types.go:68` |
| `Replicas` | `int32` | json:replicas,omitempty; +optional; +kubebuilder:default=1; +kubebuilder:validation:Minimum=0 | `operators/api/v1/application_types.go:74` |
| `ServicePort` | `int32` | json:servicePort,omitempty; +optional; +kubebuilder:default=80; +kubebuilder:validation:Minimum=1; +kubebuilder:validation:Maximum=65535 | `operators/api/v1/application_types.go:81` |


## `LoopZeroApplicationStatus`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/application_types.go:85`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Phase` | `ApplicationPhase` | json:phase,omitempty | `operators/api/v1/application_types.go:87` |
| `EndpointsReady` | `bool` | json:endpointsReady,omitempty | `operators/api/v1/application_types.go:90` |
| `Reachable` | `bool` | json:reachable,omitempty | `operators/api/v1/application_types.go:93` |
| `URL` | `string` | json:url,omitempty | `operators/api/v1/application_types.go:96` |
| `ObservedGeneration` | `int64` | json:observedGeneration,omitempty | `operators/api/v1/application_types.go:99` |
| `Conditions` | `[]metav1.Condition` | json:conditions,omitempty | `operators/api/v1/application_types.go:103` |


# Relationships

- Declared in `operators/api/v1`, the CRD type module described in
  [`/components/operators.md`](/components/operators.md). That module has **efferent coupling 0
  and afferent coupling 11** — it depends on nothing in the workspace and eleven packages depend
  on it, which is the shape a pure type module should have.
- Reconciled by one of the four operator binaries; the reconciler persists `.status` with a merge
  patch off a base snapshot, never with `Status().Update`.
- Shipped to clusters by the `loopzero-crds` chart in
  [`/components/helm-charts.md`](/components/helm-charts.md).
- The other seven roots: [`ClusterCredential`](/entities/crd-clustercredential.md), [`Environment`](/entities/crd-environment.md), [`ExposedRoute`](/entities/crd-exposedroute.md), [`GitRepositoryBinding`](/entities/crd-gitrepositorybinding.md), [`PlatformBootstrap`](/entities/crd-platformbootstrap.md), [`PreviewEnvironment`](/entities/crd-previewenvironment.md), [`Tenant`](/entities/crd-tenant.md).

# Evidence

[^crd-loopzeroapplication]: `operators/api/v1/application_types.go:39`
[^f-loopzeroapplicationlist]: `operators/api/v1/application_types.go:109`
[^f-loopzeroapplicationspec]: `operators/api/v1/application_types.go:48`
[^f-loopzeroapplicationstatus]: `operators/api/v1/application_types.go:85`
