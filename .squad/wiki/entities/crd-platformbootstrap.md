---
type: Entity
title: "PlatformBootstrap (CustomResourceDefinition)"
description: "The PlatformBootstrap CRD and the 9 supporting Go types it references, declared in operators/api/v1."
resource: "operators/api/v1"
tags:
  - crd
  - kubernetes
  - entity
  - platformbootstrap
sources:
  - id: crd-platformbootstrap
    resource: "operators/api/v1/platformbootstrap_types.go#L48"
    note: "PlatformBootstrap"
  - id: f-apibootstrapspec
    resource: "operators/api/v1/platformbootstrap_types.go#L146"
    note: "APIBootstrapSpec"
  - id: f-argocdbootstrapspec
    resource: "operators/api/v1/platformbootstrap_types.go#L107"
    note: "ArgoCDBootstrapSpec"
  - id: f-argocdrepocredential
    resource: "operators/api/v1/platformbootstrap_types.go#L137"
    note: "ArgoCDRepoCredential"
  - id: f-giteabootstrapspec
    resource: "operators/api/v1/platformbootstrap_types.go#L78"
    note: "GiteaBootstrapSpec"
  - id: f-platformbootstraplist
    resource: "operators/api/v1/platformbootstrap_types.go#L224"
    note: "PlatformBootstrapList"
  - id: f-platformbootstrapspec
    resource: "operators/api/v1/platformbootstrap_types.go#L57"
    note: "PlatformBootstrapSpec"
  - id: f-platformbootstrapstatus
    resource: "operators/api/v1/platformbootstrap_types.go#L199"
    note: "PlatformBootstrapStatus"
  - id: f-secretkeyref
    resource: "operators/api/v1/platformbootstrap_types.go#L193"
    note: "SecretKeyRef"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

`PlatformBootstrap` is one of the **eight wired CustomResourceDefinitions** in `operators/api/v1`, a Go
module of its own so the API server can depend on the types without depending on the operators.
The family below is 10 Go structs: the root, its List wrapper, its Spec and Status, and
the supporting types they reference.[^crd-platformbootstrap]


The widest CRD family here — ten structs, because bootstrap has to describe Gitea, ArgoCD, the
API and a timeout budget in one object. Its ordering is strictly sequential:
`Pending → ConfiguringGitea → ConfiguringArgoCD → WaitingForAPI → BootstrapComplete`, with
`RetryExhausted` the only path to `Failed` from the three middle states —
[`/entities/state-machine-platformbootstrap.md`](/entities/state-machine-platformbootstrap.md).


# Schema


## `APIBootstrapSpec`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/platformbootstrap_types.go:146`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `BootstrapEndpoint` | `string` | json:bootstrapEndpoint,omitempty; +kubebuilder:default="http://loopzero-api.loopzero-system.svc.cluster.local:8080/internal/bootstrap"; +kubebuilder:validation:Pattern=`^https?://[a-zA-Z0-9][a-zA-Z0-9.\-]+(:[0-9]+)?(/.*)?$`; +optional | `operators/api/v1/platformbootstrap_types.go:152` |
| `HealthEndpoint` | `string` | json:healthEndpoint,omitempty; +kubebuilder:default="http://loopzero-api.loopzero-system.svc.cluster.local:8080/api/v1/health"; +kubebuilder:validation:Pattern=`^https?://[a-zA-Z0-9][a-zA-Z0-9.\-]+(:[0-9]+)?(/.*)?$`; +optional | `operators/api/v1/platformbootstrap_types.go:159` |
| `BootstrapSecretRef` | `*SecretKeyRef` | json:bootstrapSecretRef,omitempty; +optional | `operators/api/v1/platformbootstrap_types.go:166` |


## `ArgoCDBootstrapSpec`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/platformbootstrap_types.go:107`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Namespace` | `string` | json:namespace,omitempty; +kubebuilder:default="argocd"; +optional | `operators/api/v1/platformbootstrap_types.go:111` |
| `AdminSecretName` | `string` | json:adminSecretName,omitempty; +kubebuilder:default="argocd-initial-admin-secret"; +optional | `operators/api/v1/platformbootstrap_types.go:117` |
| `RepoCredentials` | `[]ArgoCDRepoCredential` | json:repoCredentials,omitempty; +optional | `operators/api/v1/platformbootstrap_types.go:126` |
| `InternalURL` | `string` | json:internalURL,omitempty; +kubebuilder:default="https://argocd-server.argocd.svc.cluster.local"; +kubebuilder:validation:Pattern=`^https?://[a-zA-Z0-9][a-zA-Z0-9.\-]+(:[0-9]+)?(/.*)?$`; +optional | `operators/api/v1/platformbootstrap_types.go:133` |


## `ArgoCDRepoCredential`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/platformbootstrap_types.go:137`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Name` | `string` | json:name | `operators/api/v1/platformbootstrap_types.go:139` |
| `URL` | `string` | json:url | `operators/api/v1/platformbootstrap_types.go:142` |


## `GiteaBootstrapSpec`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/platformbootstrap_types.go:78`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `AdminSecretRef` | `SecretKeyRef` | json:adminSecretRef | `operators/api/v1/platformbootstrap_types.go:82` |
| `Organizations` | `[]string` | json:organizations,omitempty; +optional | `operators/api/v1/platformbootstrap_types.go:86` |
| `TokenName` | `string` | json:tokenName,omitempty; +kubebuilder:default="platform-bootstrap-token"; +optional | `operators/api/v1/platformbootstrap_types.go:92` |
| `TokenScopes` | `[]string` | json:tokenScopes,omitempty; +optional | `operators/api/v1/platformbootstrap_types.go:96` |
| `InternalURL` | `string` | json:internalURL,omitempty; +kubebuilder:default="http://gitea-http.gitea.svc.cluster.local:3000"; +kubebuilder:validation:Pattern=`^https?://[a-zA-Z0-9][a-zA-Z0-9.\-]+(:[0-9]+)?(/.*)?$`; +optional | `operators/api/v1/platformbootstrap_types.go:103` |


## `PlatformBootstrap`

2 printcolumn markers Declared at `operators/api/v1/platformbootstrap_types.go:48`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `metav1.TypeMeta` | `metav1.TypeMeta` | json:,inline; embedded | `operators/api/v1/platformbootstrap_types.go:49` |
| `metav1.ObjectMeta` | `metav1.ObjectMeta` | json:metadata,omitempty; embedded | `operators/api/v1/platformbootstrap_types.go:50` |
| `Spec` | `PlatformBootstrapSpec` | json:spec,omitempty | `operators/api/v1/platformbootstrap_types.go:52` |
| `Status` | `PlatformBootstrapStatus` | json:status,omitempty | `operators/api/v1/platformbootstrap_types.go:53` |


## `PlatformBootstrapList`

kubebuilder List wrapper for the CRD root Declared at `operators/api/v1/platformbootstrap_types.go:224`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `metav1.TypeMeta` | `metav1.TypeMeta` | json:,inline; embedded | `operators/api/v1/platformbootstrap_types.go:225` |
| `metav1.ListMeta` | `metav1.ListMeta` | json:metadata,omitempty; embedded | `operators/api/v1/platformbootstrap_types.go:226` |
| `Items` | `[]PlatformBootstrap` | json:items | `operators/api/v1/platformbootstrap_types.go:227` |


## `PlatformBootstrapSpec`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/platformbootstrap_types.go:57`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Gitea` | `GiteaBootstrapSpec` | json:gitea | `operators/api/v1/platformbootstrap_types.go:59` |
| `ArgoCD` | `ArgoCDBootstrapSpec` | json:argocd | `operators/api/v1/platformbootstrap_types.go:62` |
| `API` | `APIBootstrapSpec` | json:api | `operators/api/v1/platformbootstrap_types.go:65` |
| `MaxRetries` | `int` | json:maxRetries,omitempty; +kubebuilder:default=10; +optional | `operators/api/v1/platformbootstrap_types.go:74` |


## `PlatformBootstrapStatus`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/platformbootstrap_types.go:199`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Phase` | `PlatformBootstrapPhase` | json:phase,omitempty; +optional | `operators/api/v1/platformbootstrap_types.go:202` |
| `RetryCount` | `int` | json:retryCount,omitempty; +optional | `operators/api/v1/platformbootstrap_types.go:206` |
| `ObservedGeneration` | `int64` | json:observedGeneration,omitempty; +optional | `operators/api/v1/platformbootstrap_types.go:210` |
| `Conditions` | `[]metav1.Condition` | json:conditions,omitempty; +optional | `operators/api/v1/platformbootstrap_types.go:214` |
| `LastTransitionTime` | `*metav1.Time` | json:lastTransitionTime,omitempty; +optional | `operators/api/v1/platformbootstrap_types.go:218` |


## `SecretKeyRef`

Go struct in operators/api/v1 (CRD API group, own Go module mod.operators.api) Declared at `operators/api/v1/platformbootstrap_types.go:193`.

| Field | Type | Marker / json tag | Anchor |
|---|---|---|---|
| `Name` | `string` | json:name | `operators/api/v1/platformbootstrap_types.go:195` |


# Relationships

- Declared in `operators/api/v1`, the CRD type module described in
  [`/components/operators.md`](/components/operators.md). That module has **efferent coupling 0
  and afferent coupling 11** — it depends on nothing in the workspace and eleven packages depend
  on it, which is the shape a pure type module should have.
- Reconciled by one of the four operator binaries; the reconciler persists `.status` with a merge
  patch off a base snapshot, never with `Status().Update`.
- Shipped to clusters by the `loopzero-crds` chart in
  [`/components/helm-charts.md`](/components/helm-charts.md).
- The other seven roots: [`ClusterCredential`](/entities/crd-clustercredential.md), [`Environment`](/entities/crd-environment.md), [`ExposedRoute`](/entities/crd-exposedroute.md), [`GitRepositoryBinding`](/entities/crd-gitrepositorybinding.md), [`PreviewEnvironment`](/entities/crd-previewenvironment.md), [`Tenant`](/entities/crd-tenant.md), [`LoopZeroApplication`](/entities/crd-loopzeroapplication.md).

# Evidence

[^crd-platformbootstrap]: `operators/api/v1/platformbootstrap_types.go:48`
[^f-apibootstrapspec]: `operators/api/v1/platformbootstrap_types.go:146`
[^f-argocdbootstrapspec]: `operators/api/v1/platformbootstrap_types.go:107`
[^f-argocdrepocredential]: `operators/api/v1/platformbootstrap_types.go:137`
[^f-giteabootstrapspec]: `operators/api/v1/platformbootstrap_types.go:78`
[^f-platformbootstraplist]: `operators/api/v1/platformbootstrap_types.go:224`
[^f-platformbootstrapspec]: `operators/api/v1/platformbootstrap_types.go:57`
[^f-platformbootstrapstatus]: `operators/api/v1/platformbootstrap_types.go:199`
[^f-secretkeyref]: `operators/api/v1/platformbootstrap_types.go:193`
