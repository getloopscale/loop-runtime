---
type: Operations
title: "What runs where — three layers, five clusters"
description: "The L1/L2/L3 layering and the component-to-cluster assignment, read from the helmfiles rather than from a diagram."
resource: "infra"
tags:
  - deployment
  - topology
  - helmfile
  - argocd
sources:
  - id: dt-core
    resource: "infra/k3d/core-cluster.yaml#L4-L10"
    note: "loopzero-core (k3d cluster)"
  - id: dt-hf
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L356"
    note: "Helmfile (L2 bootstrap)"
  - id: dt-pulumi
    resource: "infra/pulumi/README.md#L359"
    note: "Pulumi monolith (L1 cloud primitives, DigitalOcean)"
  - id: dt-argocd
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L356-L361"
    note: "ArgoCD (L3 GitOps delivery)"
  - id: dt-l1l2
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L1-L16"
    note: "L2 consumes L1 outputs: LOOPZERO_DB_*, LOOPZERO_REGISTRY_*, LOOPZERO_BUCKET_*, LOOPZERO_CF_* are injected into the helmfile from Pulumi stack outputs"
  - id: dt-l3
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L356"
    note: "ArgoCD L3 syncs tenant Day-2 state FROM the Gitea repositories the API pushes to — the two halves of the GitOps loop"
  - id: dt-churn
    resource: "infra/pulumi/README.md#L359"
    note: "infra/pulumi is declared AS-IS legacy yet is the most-churned infra directory, and no successor exists in this repo"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

Three layers, each owning a different question, and five clusters.

| Layer | Owns | Tooling | Source of truth |
|---|---|---|---|
| **L1** cloud primitives | managed Kubernetes, database, registry, buckets, DNS | Pulumi / DigitalOcean (legacy) | `infra/pulumi*`[^dt-pulumi] |
| **L2** platform | ~32 platform releases across the planes | Helmfile | `infra/helmfile/*.gotmpl`[^dt-hf] |
| **L3** tenant Day-2 | tenant workloads | ArgoCD, **manual sync**, from Gitea | the tenant GitOps repos[^dt-l3] |

L2 **consumes L1's outputs** — `LOOPZERO_DB_*`, `LOOPZERO_REGISTRY_*`, `LOOPZERO_BUCKET_*`, `LOOPZERO_CF_*` are
injected as environment.[^dt-l1l2] L2 also **installs L3**: ArgoCD is itself a helmfile release.

The in-cluster layers (L2, L3, the charts, the k3d dev topology) are unchanged by the declared
AWS re-platform; only L1 is in scope for it.

# Schema


## control — `infra/k3d/core-cluster.yaml`

_28 components deployed here._

| Component | Kind | Declared at |
|---|---|---|
| `chart.loopzero_app_operator` | artifact | `infra/helmfile/core.helmfile.yaml.gotmpl:585-589` |
| `chart.loopzero_platform_operator` | artifact | `infra/helmfile/core.helmfile.yaml.gotmpl:625-629` |
| `chart.loopzero_preview_operator` | artifact | `infra/helmfile/core.helmfile.yaml.gotmpl:605-609` |
| `chart.loopzero_tenant_operator` | artifact | `infra/helmfile/core.helmfile.yaml.gotmpl:560-564` |
| `ds.gitea` | datastore | `infra/helmfile/core.helmfile.yaml.gotmpl:259-264` |
| `ds.harbor` | datastore | `infra/helmfile/core.helmfile.yaml.gotmpl:438-448` |
| `ds.minio` | datastore | `infra/helmfile/core.helmfile.yaml.gotmpl:299-304` |
| `ds.postgres` | datastore | `infra/helmfile/core.helmfile.yaml.gotmpl:220-231` |
| `ds.prometheus` | datastore | `infra/helmfile/core.helmfile.yaml.gotmpl:369-372` |
| `ds.tempo` | datastore | `infra/helmfile/core.helmfile.yaml.gotmpl:404-407` |
| `infra.admission_policies` | runtime | `infra/helmfile/core.helmfile.yaml.gotmpl:766-768` |
| `infra.cert_manager` | runtime | `infra/helmfile/core.helmfile.yaml.gotmpl:145-150` |
| `infra.cert_manager` | runtime | `infra/helmfile/core.helmfile.yaml.gotmpl:145-150` |
| `infra.external_secrets` | runtime | `infra/helmfile/core.helmfile.yaml.gotmpl:701-704` |
| `infra.external_secrets` | runtime | `infra/helmfile/core.helmfile.yaml.gotmpl:701-704` |
| `infra.grafana` | runtime | `infra/helmfile/core.helmfile.yaml.gotmpl:380-383` |
| `infra.ory_hydra` | service | `infra/helmfile/core.helmfile.yaml.gotmpl:480-485` |
| `infra.ory_kratos` | service | `infra/helmfile/core.helmfile.yaml.gotmpl:465-470` |
| `infra.otel_collector` | stream | `infra/helmfile/core.helmfile.yaml.gotmpl:411-414` |
| `infra.redis.gitea` | cache | `infra/helmfile/core.helmfile.yaml.gotmpl:245-255` |
| `infra.release.loopzero_api` | service | `infra/helmfile/core.helmfile.yaml.gotmpl:494-507` |
| `infra.release.loopzero_cloud` | service | `infra/helmfile/core.helmfile.yaml.gotmpl:645-654` |
| `infra.release.loopzero_crds` | artifact | `infra/helmfile/core.helmfile.yaml.gotmpl:182-186` |
| `infra.smoke_test` | batch_job | `infra/helmfile/core.helmfile.yaml.gotmpl:722-741` |
| `infra.loopzero_external_secrets` | runtime | `infra/helmfile/core.helmfile.yaml.gotmpl:701-704` |
| `infra.traefik` | runtime | `infra/helmfile/core.helmfile.yaml.gotmpl:192-197` |
| `infra.velero` | batch_job | `infra/helmfile/core.helmfile.yaml.gotmpl:332-353` |
| `orch.argocd` | orchestrator | `infra/helmfile/core.helmfile.yaml.gotmpl:356-361` |

## build — `infra/k3d/build-cluster.yaml`

_5 components deployed here._

| Component | Kind | Declared at |
|---|---|---|
| `ds.loki` | datastore | `infra/helmfile/build.helmfile.yaml.gotmpl:157-160` |
| `infra.promtail` | stream | `infra/helmfile/build.helmfile.yaml.gotmpl:164-167` |
| `orch.argo_events` | orchestrator | `infra/helmfile/build.helmfile.yaml.gotmpl:113-115` |
| `orch.argo_workflows` | orchestrator | `infra/helmfile/build.helmfile.yaml.gotmpl:68-71` |
| `orch.build_cwts` | batch_job | `infra/helmfile/build.helmfile.yaml.gotmpl:81-83` |

## cell-0 — `infra/k3d/cell-0-cluster.yaml`

_11 components deployed here._

| Component | Kind | Declared at |
|---|---|---|
| `infra.admission_policies` | runtime | `infra/helmfile/cell.helmfile.yaml.gotmpl:192-194` |
| `infra.cert_manager` | runtime | `infra/helmfile/cell.helmfile.yaml.gotmpl:99-102` |
| `infra.cloudflared` | runtime | `infra/helmfile/cell.helmfile.yaml.gotmpl:247-249` |
| `infra.cluster_issuer` | runtime | `infra/helmfile/cell.helmfile.yaml.gotmpl:257-259` |
| `infra.external_secrets` | runtime | `infra/helmfile/cell.helmfile.yaml.gotmpl:131-134` |
| `infra.policy_controller` | runtime | `infra/helmfile/cell.helmfile.yaml.gotmpl:179-185` |
| `infra.promtail` | stream | `infra/helmfile/cell.helmfile.yaml.gotmpl:236-239` |
| `infra.static_assets` | runtime | `infra/helmfile/cell.helmfile.yaml.gotmpl:277-279` |
| `infra.traefik` | runtime | `infra/helmfile/cell.helmfile.yaml.gotmpl:121-124` |
| `orch.argo_rollouts` | orchestrator | `infra/helmfile/cell.helmfile.yaml.gotmpl:171-173` |
| `orch.keda` | orchestrator | `infra/helmfile/cell.helmfile.yaml.gotmpl:146-149` |

## cell-1 — `infra/k3d/cell-1-cluster.yaml`

_11 components deployed here._

| Component | Kind | Declared at |
|---|---|---|
| `infra.admission_policies` | runtime | `infra/helmfile/cell.helmfile.yaml.gotmpl:192-194` |
| `infra.cert_manager` | runtime | `infra/helmfile/cell.helmfile.yaml.gotmpl:99-102` |
| `infra.cloudflared` | runtime | `infra/helmfile/cell.helmfile.yaml.gotmpl:247-249` |
| `infra.cluster_issuer` | runtime | `infra/helmfile/cell.helmfile.yaml.gotmpl:257-259` |
| `infra.external_secrets` | runtime | `infra/helmfile/cell.helmfile.yaml.gotmpl:131-134` |
| `infra.policy_controller` | runtime | `infra/helmfile/cell.helmfile.yaml.gotmpl:179-185` |
| `infra.promtail` | stream | `infra/helmfile/cell.helmfile.yaml.gotmpl:236-239` |
| `infra.static_assets` | runtime | `infra/helmfile/cell.helmfile.yaml.gotmpl:277-279` |
| `infra.traefik` | runtime | `infra/helmfile/cell.helmfile.yaml.gotmpl:121-124` |
| `orch.argo_rollouts` | orchestrator | `infra/helmfile/cell.helmfile.yaml.gotmpl:171-173` |
| `orch.keda` | orchestrator | `infra/helmfile/cell.helmfile.yaml.gotmpl:146-149` |

## secrets — `infra/k3d/secrets-cluster.yaml`

_4 components deployed here._

| Component | Kind | Declared at |
|---|---|---|
| `ds.openbao` | datastore | `infra/helmfile/secrets.helmfile.yaml.gotmpl:118-122` |
| `infra.cert_manager` | runtime | `infra/helmfile/secrets.helmfile.yaml.gotmpl:72-75` |
| `infra.cluster_issuer` | runtime | `infra/helmfile/secrets.helmfile.yaml.gotmpl:91-93` |
| `infra.openbao_snapshot` | batch_job | `infra/helmfile/secrets.helmfile.yaml.gotmpl:383-386` |

# How it is stood up

```
task doctor     environment health check — run this first
task setup      ONE-SHOT cold start: 4 k3d clusters + Helmfile sync (~30-45 min)
tilt up         DEV LOOP: hot reload of API and operators
task status     clusters, pods, DB, API at a glance
```

The secrets cluster is created separately (`task k3d:secrets:create` +
`k3d:secrets:bootstrap`) and is conditional — see
[`/components/cluster-topology.md`](/components/cluster-topology.md).

# Relationships

- The releases and their `needs:` ordering are
  [`/components/helmfile-layer.md`](/components/helmfile-layer.md).
- The charts installed are [`/components/helm-charts.md`](/components/helm-charts.md).
- The stateful services are
  [`/components/platform-datastores.md`](/components/platform-datastores.md); the controllers are
  [`/components/platform-orchestrators.md`](/components/platform-orchestrators.md).
- A tenant's workloads land on the cell chosen by
  [`/flows/tenant-cell-placement.md`](/flows/tenant-cell-placement.md).

# Where the code and the documents disagree

The L1 layer is declared **AS-IS legacy pending a Terraform cutover**, and measured is the
**most-churned** infrastructure tree in the repository — 351 commits against 214 for
`infra/helm`, 62 for `infra/helmfile` and 28 for `infra/k3d`, with its most recent commit on the
day of this map.[^dt-churn] No `.tf` file exists anywhere in this repository. The full
four-part disagreement, including a half-executed ADR and a deleted project that still appears
on disk, is [`/components/pulumi-legacy.md`](/components/pulumi-legacy.md).

# Evidence

[^dt-core]: `infra/k3d/core-cluster.yaml:4-10`
[^dt-hf]: `infra/helmfile/core.helmfile.yaml.gotmpl:356`
[^dt-pulumi]: `infra/pulumi/README.md:359`
[^dt-argocd]: `infra/helmfile/core.helmfile.yaml.gotmpl:356-361`
[^dt-l1l2]: `infra/helmfile/core.helmfile.yaml.gotmpl:1-16`
[^dt-l3]: `infra/helmfile/core.helmfile.yaml.gotmpl:356`
[^dt-churn]: `infra/pulumi/README.md:359`
