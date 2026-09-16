---
type: Entity
title: "ExposedRoute (CustomResourceDefinition)"
description: "The ExposedRoute CRD and the 5 supporting Go types it references, declared in operators/api/v1."
resource: "operators/api/v1"
tags:
  - crd
  - kubernetes
  - entity
  - exposedroute
sources:
  - id: crd-exposedroute
    resource: "operators/api/v1/exposedroute_types.go#L30"
    note: "ExposedRoute"
  - id: f-exposedroutelist
    resource: "operators/api/v1/exposedroute_types.go#L108"
    note: "ExposedRouteList"
  - id: f-exposedroutespec
    resource: "operators/api/v1/exposedroute_types.go#L39"
    note: "ExposedRouteSpec"
  - id: f-exposedroutestatus
    resource: "operators/api/v1/exposedroute_types.go#L81"
    note: "ExposedRouteStatus"
  - id: f-serviceref
    resource: "operators/api/v1/exposedroute_types.go#L57"
    note: "ServiceRef"
  - id: f-tlsconfig
    resource: "operators/api/v1/exposedroute_types.go#L70"
    note: "TLSConfig"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

`ExposedRoute` is one of the **eight wired CustomResourceDefinitions** in `operators/api/v1`, a Go
module of its own so the API server can depend on the types without depending on the operators.
The family below is 6 Go structs: the root, its List wrapper, its Spec and Status, and
the supporting types they reference.[^crd-exposedroute]


Route readiness is probe-driven rather than declared: `Pending → CertIssued → Reachable`, with a
failed probe sending any state back to `Failed`. The states are
[`/entities/state-machine-exposedroute.md`](/entities/state-machine-exposedroute.md).

For Cloudflare-managed domains no cert-manager `Certificate` is created — CF terminates TLS, and
creating one anyway is the documented mistake.


# Schema


## `ExposedRoute`

3 printcolumn markers Declared at `operators/api/v1/exposedroute_types.go:30`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `metav1.TypeMeta` | `metav1.TypeMeta` | json:,inline; embedded | `operators/api/v1/exposedroute_types.go:31` |
| `metav1.ObjectMeta` | `metav1.ObjectMeta` | json:metadata,omitempty; embedded | `operators/api/v1/exposedroute_types.go:32` |
| `Spec` | `ExposedRouteSpec` | json:spec,omitempty | `operators/api/v1/exposedroute_types.go:34` |
| `Status` | `ExposedRouteStatus` | json:status,omitempty | `operators/api/v1/exposedroute_types.go:35` |


## `ExposedRouteList`

kubebuilder List wrapper for the CRD root Declared at `operators/api/v1/exposedroute_types.go:108`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `metav1.TypeMeta` | `metav1.TypeMeta` | json:,inline; embedded | `operators/api/v1/exposedroute_types.go:109` |
| `metav1.ListMeta` | `metav1.ListMeta` | json:metadata,omitempty; embedded | `operators/api/v1/exposedroute_types.go:110` |
| `Items` | `[]ExposedRoute` | json:items | `operators/api/v1/exposedroute_types.go:111` |


## `ExposedRouteSpec`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/exposedroute_types.go:39`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `ApplicationRef` | `string` | json:applicationRef; +kubebuilder:validation:MinLength=1 | `operators/api/v1/exposedroute_types.go:42` |
| `Hostname` | `string` | json:hostname; +kubebuilder:validation:MinLength=1 | `operators/api/v1/exposedroute_types.go:46` |
| `ServiceRef` | `ServiceRef` | json:serviceRef | `operators/api/v1/exposedroute_types.go:49` |
| `TLS` | `TLSConfig` | json:tls,omitempty; +optional | `operators/api/v1/exposedroute_types.go:53` |


## `ExposedRouteStatus`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/exposedroute_types.go:81`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Phase` | `ExposedRoutePhase` | json:phase,omitempty | `operators/api/v1/exposedroute_types.go:83` |
| `CertificateReady` | `bool` | json:certificateReady,omitempty | `operators/api/v1/exposedroute_types.go:86` |
| `LastProbeTime` | `*metav1.Time` | json:lastProbeTime,omitempty; +optional | `operators/api/v1/exposedroute_types.go:90` |
| `LastProbeResult` | `int32` | json:lastProbeResult,omitempty | `operators/api/v1/exposedroute_types.go:93` |
| `ConsecutiveFailures` | `int32` | json:consecutiveFailures,omitempty | `operators/api/v1/exposedroute_types.go:96` |
| `ObservedGeneration` | `int64` | json:observedGeneration,omitempty | `operators/api/v1/exposedroute_types.go:99` |
| `Conditions` | `[]metav1.Condition` | json:conditions,omitempty | `operators/api/v1/exposedroute_types.go:102` |


## `ServiceRef`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/exposedroute_types.go:57`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Name` | `string` | json:name; +kubebuilder:validation:MinLength=1 | `operators/api/v1/exposedroute_types.go:60` |
| `Port` | `int32` | json:port; +kubebuilder:default=80; +kubebuilder:validation:Minimum=1; +kubebuilder:validation:Maximum=65535 | `operators/api/v1/exposedroute_types.go:66` |


## `TLSConfig`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/exposedroute_types.go:70`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Enabled` | `bool` | json:enabled,omitempty; +optional | `operators/api/v1/exposedroute_types.go:73` |
| `IssuerRef` | `string` | json:issuerRef,omitempty; +optional | `operators/api/v1/exposedroute_types.go:77` |


# Relationships

- Declared in `operators/api/v1`, the CRD type module described in
  [`/components/operators.md`](/components/operators.md). That module has **efferent coupling 0
  and afferent coupling 11** — it depends on nothing in the workspace and eleven packages depend
  on it, which is the shape a pure type module should have.
- Reconciled by one of the four operator binaries; the reconciler persists `.status` with a merge
  patch off a base snapshot, never with `Status().Update`.
- Shipped to clusters by the `loopzero-crds` chart in
  [`/components/helm-charts.md`](/components/helm-charts.md).
- The other seven roots: [`ClusterCredential`](/entities/crd-clustercredential.md), [`Environment`](/entities/crd-environment.md), [`GitRepositoryBinding`](/entities/crd-gitrepositorybinding.md), [`PlatformBootstrap`](/entities/crd-platformbootstrap.md), [`PreviewEnvironment`](/entities/crd-previewenvironment.md), [`Tenant`](/entities/crd-tenant.md), [`LoopZeroApplication`](/entities/crd-loopzeroapplication.md).

# Evidence

[^crd-exposedroute]: `operators/api/v1/exposedroute_types.go:30`
[^f-exposedroutelist]: `operators/api/v1/exposedroute_types.go:108`
[^f-exposedroutespec]: `operators/api/v1/exposedroute_types.go:39`
[^f-exposedroutestatus]: `operators/api/v1/exposedroute_types.go:81`
[^f-serviceref]: `operators/api/v1/exposedroute_types.go:57`
[^f-tlsconfig]: `operators/api/v1/exposedroute_types.go:70`
