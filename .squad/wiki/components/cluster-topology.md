---
type: Component
title: "Five clusters — the zone topology"
description: "Control, build, two runtime cells and a separate secrets cluster, each defined by its own k3d manifest."
resource: "infra/k3d"
tags:
  - kubernetes
  - k3d
  - topology
  - cluster
sources:
  - id: ct-core
    resource: "infra/k3d/core-cluster.yaml#L4"
    note: "Control plane (core cluster)"
  - id: ct-build
    resource: "infra/k3d/build-cluster.yaml#L4"
    note: "Build plane (build cluster)"
  - id: ct-c0
    resource: "infra/k3d/cell-0-cluster.yaml#L4"
    note: "Workload plane cell-0"
  - id: ct-c1
    resource: "infra/k3d/cell-1-cluster.yaml#L4"
    note: "Workload plane cell-1"
  - id: ct-sec
    resource: "infra/k3d/secrets-cluster.yaml#L1"
    note: "Secrets plane (separate cluster)"
  - id: ct-hcore
    resource: "infra/k3d/core-cluster.yaml#L4-L10"
    note: "loopzero-core (k3d cluster)"
  - id: ct-hsec
    resource: "infra/k3d/secrets-cluster.yaml#L36-L42"
    note: "loopzero-secrets (k3d cluster)"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

Five Kubernetes clusters, each with a k3d manifest of its own. Four are created by `task setup`;
the fifth — secrets — is created by a separate task and is **conditional**.[^ct-sec]

| Zone | k3d manifest | Plane | Components deployed here |
|---|---|---|---|
| `zone.control` | `infra/k3d/core-cluster.yaml` | control | 28 |
| `zone.build` | `infra/k3d/build-cluster.yaml` | build | 5 |
| `zone.cell0` | `infra/k3d/cell-0-cluster.yaml` | workload | 11 |
| `zone.cell1` | `infra/k3d/cell-1-cluster.yaml` | workload | 11 |
| `zone.secrets` | `infra/k3d/secrets-cluster.yaml` | secrets | 4 |

The filenames on disk are `core` / `build` / `cell-0` / `cell-1` / `secrets`. The older names
`mgmt`, `workload` and `runtime-2` appear in **no** filename.

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `zone.control` | `infra/k3d/core-cluster.yaml` | zone | infrastructure | `infra/k3d/core-cluster.yaml:4` | k3d cluster `loopzero-core`, 1 server + 2 agents, kubeAPI on host port 6443. Hosts the releases declared in core.helmfile.yaml.gotmpl: postgresql, gitea, gitea-redis, minio, velero, argocd, harbor, harbor-redis, prometheus, grafana, tempo, otel-collector, opencost, blackbox-exporter, nfs-server-provisioner, cert-manager, traefik, loopzero-crds. |
| `zone.build` | `infra/k3d/build-cluster.yaml` | zone | infrastructure | `infra/k3d/build-cluster.yaml:4` | k3d cluster `loopzero-build`, 1 server + 1 agent, kubeAPI on host port 6445. Hosts argo-workflows, loopzero-build-cwts, argo-events, argo-events-bus, argo-ci-namespace, loki and promtail per build.helmfile.yaml.gotmpl. |
| `zone.cell0` | `infra/k3d/cell-0-cluster.yaml` | zone | infrastructure | `infra/k3d/cell-0-cluster.yaml:4` | k3d cluster `loopzero-cell-0`, kubeAPI on 6444, Traefik published on host 8090/8443. A runtime cell: user applications land here via ArgoCD. |
| `zone.cell1` | `infra/k3d/cell-1-cluster.yaml` | zone | infrastructure | `infra/k3d/cell-1-cluster.yaml:4` | k3d cluster `loopzero-cell-1`, kubeAPI on 6446, Traefik on host 8091/8444. Its own header records that it pairs with the `cell-1` row in the cells SQL table (migration 000006) and only becomes a routing target once cells.status flips from 'maintenance' to 'active'. |
| `zone.secrets` | `infra/k3d/secrets-cluster.yaml` | zone | infrastructure | `infra/k3d/secrets-cluster.yaml:1` | k3d cluster for OpenBao, single server, kubeAPI on 6447, Vault API published on host 8200. A FIFTH cluster, separate from core by design. Its header is written in Portuguese. |
| `host.k3d.core` | `infra/k3d/core-cluster.yaml` | host | host | `infra/k3d/core-cluster.yaml:4-10` | Control plane cluster: 1 server + 2 agents, kubeAPI hostPort 6443. Carries the L2 platform releases declared by core.helmfile. |
| `host.k3d.build` | `infra/k3d/build-cluster.yaml` | host | host | `infra/k3d/build-cluster.yaml:4-12` | Build plane cluster: 1 server + 1 agent, kubeAPI hostPort 6445. Argo Workflows + BuildKit. |
| `host.k3d.cell_0` | `infra/k3d/cell-0-cluster.yaml` | host | host | `infra/k3d/cell-0-cluster.yaml:4-12` | Workload cell 0: 1 server + 1 agent, kubeAPI hostPort 6444, Traefik on host 8090/8443. |
| `host.k3d.cell_1` | `infra/k3d/cell-1-cluster.yaml` | host | host | `infra/k3d/cell-1-cluster.yaml:4-30` | Workload cell 1: 1 server + 1 agent, kubeAPI hostPort 6446, Traefik on host 8091/8444. |
| `host.k3d.secrets` | `infra/k3d/secrets-cluster.yaml` | host | host | `infra/k3d/secrets-cluster.yaml:36-42` | Dedicated secrets cluster: 1 server, 0 agents, kubeAPI hostPort 6447, OpenBao API on 8200. Separate by topology decision; installing OpenBao into core is forbidden. |

# Relationships

- Each zone **contains** the releases its plane's helmfile declares — the mapping is in
  [`/components/helmfile-layer.md`](/components/helmfile-layer.md).
- The control cluster holds Postgres, Gitea, MinIO, Harbor, ArgoCD and both Redis instances; the
  stateful set is [`/components/platform-datastores.md`](/components/platform-datastores.md).
- The build cluster holds Argo Workflows, Argo Events, the build ClusterWorkflowTemplates and
  Loki — nothing tenant-facing runs there.
- Each runtime cell holds Traefik, cert-manager, KEDA, Argo Rollouts and ESO, and is where
  tenant workloads actually run. Which cell a tenant lands in is decided once, by
  [`/flows/tenant-cell-placement.md`](/flows/tenant-cell-placement.md).
- The API reaches every cell through the credentials in
  [`/components/multi-cell-routing.md`](/components/multi-cell-routing.md), looked up by
  `spec.cellID`.

## Why the secrets cluster is separate, and why it is conditional

OpenBao runs in a **fifth, separate cluster**. Installing it into core is a topology violation,
and the shortcut task that did it was removed — a gate now fails on its existence or on the
equivalent effect under another name.

It is conditional in the strict sense: three releases in `secrets.helmfile.yaml.gotmpl` are
gated on `openbaoEnabled`, default **false**, and that flag is set in exactly one of eight
environment files. A self-hoster bringing their own secret store runs a **four-cluster**
topology, and the anti-lock-in position makes ESO recommended rather than required.

# Evidence

[^ct-core]: `infra/k3d/core-cluster.yaml:4`
[^ct-build]: `infra/k3d/build-cluster.yaml:4`
[^ct-c0]: `infra/k3d/cell-0-cluster.yaml:4`
[^ct-c1]: `infra/k3d/cell-1-cluster.yaml:4`
[^ct-sec]: `infra/k3d/secrets-cluster.yaml:1`
[^ct-hcore]: `infra/k3d/core-cluster.yaml:4-10`
[^ct-hsec]: `infra/k3d/secrets-cluster.yaml:36-42`
