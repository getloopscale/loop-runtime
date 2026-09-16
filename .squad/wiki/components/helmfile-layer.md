---
type: Component
title: "Helmfile — the L2 platform layer"
description: "One helmfile per plane, declaring every platform release and the ordering constraints between them."
resource: "infra/helmfile"
tags:
  - helmfile
  - l2
  - platform
  - iac
sources:
  - id: hf-root
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L356"
    note: "Helmfile (L2 bootstrap)"
  - id: hf-core
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L1-L4"
    note: "core.helmfile"
  - id: hf-build
    resource: "infra/helmfile/build.helmfile.yaml.gotmpl#L1-L4"
    note: "build.helmfile"
  - id: hf-cell
    resource: "infra/helmfile/cell.helmfile.yaml.gotmpl#L1-L4"
    note: "cell.helmfile"
  - id: hf-secrets
    resource: "infra/helmfile/secrets.helmfile.yaml.gotmpl#L1-L4"
    note: "secrets.helmfile"
  - id: hf-api
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L494-L507"
    note: "loopzero-api (helm release)"
  - id: hf-l1
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L1-L16"
    note: "L2 consumes L1 outputs: LOOPZERO_DB_*, LOOPZERO_REGISTRY_*, LOOPZERO_BUCKET_*, LOOPZERO_CF_* are injected into the helmfile from Pulumi stack outputs"
  - id: hf-3p
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L82-L127"
    note: "every pinned upstream chart is pulled from a third-party repository at sync time"
  - id: hf-pair
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L494-L498"
    note: "parity pair: this release installs ../helm/<name>; charts/<name> is the published OCI copy of the same chart name, and infra/quality/chart-pairs.txt declares th"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

The declarative source of truth for what runs on each cluster. Four plane-scoped files —
`core`, `build`, `cell`, `secrets` — each declaring that plane's Helm releases and their
`needs:` ordering.[^hf-root]

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `infra.helmfile` | `infra/helmfile` | orchestrator | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:356` | Declarative source of truth for platform releases across the 4 cluster roles (core/build/cell/secrets). Header states it replaces infra/pulumi/src/components/mgmt-bootstrap.ts and that Pulumi (L1) provisions the cluster this installs into. |
| `infra.helmfile.core` | `infra/helmfile/core.helmfile.yaml.gotmpl` | module | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:1-4` | Control-plane releases: datastores, ArgoCD, observability, Ory, operators. |
| `infra.helmfile.build` | `infra/helmfile/build.helmfile.yaml.gotmpl` | module | l2-platform | `infra/helmfile/build.helmfile.yaml.gotmpl:1-4` | Build-plane releases: Argo Workflows, CWTs, Argo Events, Loki/Promtail. |
| `infra.helmfile.cell` | `infra/helmfile/cell.helmfile.yaml.gotmpl` | module | l2-platform | `infra/helmfile/cell.helmfile.yaml.gotmpl:1-4` | Workload-cell releases: Traefik, KEDA, Argo Rollouts, policy-controller, VAPs. |
| `infra.helmfile.secrets` | `infra/helmfile/secrets.helmfile.yaml.gotmpl` | module | l2-platform | `infra/helmfile/secrets.helmfile.yaml.gotmpl:1-4` | Secrets-cluster releases: OpenBao + init + snapshot agent, all flag-gated. |
| `infra.release.loopzero_api` | `infra/helmfile/core.helmfile.yaml.gotmpl` | service | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:494-507` | Helmfile release 'loopzero-api', in-repo chart ../helm/loopzero-api. NOT installed in the standalone profile, where Tilt owns the deployment instead. needs: argocd, gitea, minio, ory-kratos. |
| `infra.release.loopzero_cloud` | `infra/helmfile/core.helmfile.yaml.gotmpl` | service | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:645-654` | Helmfile release 'loopzero-cloud', in-repo chart ../helm/loopzero-cloud. Skipped in drill and in the standalone profile. |
| `infra.release.loopzero_crds` | `infra/helmfile/core.helmfile.yaml.gotmpl` | artifact | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:182-186` | Helmfile release 'loopzero-crds', published chart ../../charts/loopzero-crds — the CustomResourceDefinition set every operator reconciles against, installed before them. |

## What each plane declares

| Helmfile | Declares | Notable releases |
|---|---|---|
| `core.helmfile.yaml.gotmpl` | 24 release edges | Postgres, Gitea, MinIO, Harbor, ArgoCD, cert-manager, ESO, Ory Kratos + Hydra, `loopzero-api`, `loopzero-cloud`, the four operator charts |
| `build.helmfile.yaml.gotmpl` | 5 | Argo Workflows, Argo Events, build CWTs, Loki, Promtail |
| `cell.helmfile.yaml.gotmpl` | 8 | Traefik, KEDA, Argo Rollouts, cluster-issuer, policy controller, admission policies, cloudflared, static assets |
| `secrets.helmfile.yaml.gotmpl` | 2 | OpenBao, its snapshot agent |

# Relationships

- L2 **depends on** L1: `LOOPZERO_DB_*`, `LOOPZERO_REGISTRY_*`, `LOOPZERO_BUCKET_*` and `LOOPZERO_CF_*` are
  injected from the Pulumi stack outputs described in
  [`/components/pulumi-legacy.md`](/components/pulumi-legacy.md).[^hf-l1]
- L2 installs the L3 orchestrator: ArgoCD itself is a helmfile release, and it then syncs tenant
  state from Gitea — see [`/components/platform-orchestrators.md`](/components/platform-orchestrators.md).
- Every pinned upstream chart is **pulled from a third-party repository at sync time**, which is
  the layer's one boundary-crossing dependency.[^hf-3p]
- `infra.release.loopzero_api` installs `../helm/loopzero-api`, the bootstrap half of the declared
  parity pair in [`/components/helm-charts.md`](/components/helm-charts.md).[^hf-pair]
- Which plane maps to which cluster is [`/components/cluster-topology.md`](/components/cluster-topology.md).

## Ordering is declared, not assumed

The `needs:` edges are real constraints and the map records **13** of them, out of 44
`depends_on` edges overall — the rest are Go module `replace` directives and chart dependencies.
`loopzero-api` needs ArgoCD,
Gitea, MinIO and Ory Kratos before it starts; Gitea needs Redis, Traefik and Postgres; Harbor
needs MinIO and its own Redis; ArgoCD needs cert-manager and Traefik.

# Evidence

[^hf-root]: `infra/helmfile/core.helmfile.yaml.gotmpl:356`
[^hf-core]: `infra/helmfile/core.helmfile.yaml.gotmpl:1-4`
[^hf-build]: `infra/helmfile/build.helmfile.yaml.gotmpl:1-4`
[^hf-cell]: `infra/helmfile/cell.helmfile.yaml.gotmpl:1-4`
[^hf-secrets]: `infra/helmfile/secrets.helmfile.yaml.gotmpl:1-4`
[^hf-api]: `infra/helmfile/core.helmfile.yaml.gotmpl:494-507`
[^hf-l1]: `infra/helmfile/core.helmfile.yaml.gotmpl:1-16`
[^hf-3p]: `infra/helmfile/core.helmfile.yaml.gotmpl:82-127`
[^hf-pair]: `infra/helmfile/core.helmfile.yaml.gotmpl:494-498`
