---
type: Component
title: "Orchestrators — ArgoCD, Argo Workflows, Rollouts, Events, KEDA"
description: "The six controllers that turn declared state into running workloads, split across the control, build and cell planes."
resource: "infra/helmfile"
tags:
  - argocd
  - argo-workflows
  - argo-rollouts
  - keda
  - gitops
sources:
  - id: po-argocd
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L356-L361"
    note: "ArgoCD (L3 GitOps delivery)"
  - id: po-wf
    resource: "infra/helmfile/build.helmfile.yaml.gotmpl#L68-L71"
    note: "Argo Workflows (build plane)"
  - id: po-ev
    resource: "infra/helmfile/build.helmfile.yaml.gotmpl#L113-L115"
    note: "Argo Events"
  - id: po-ro
    resource: "infra/helmfile/cell.helmfile.yaml.gotmpl#L171-L173"
    note: "Argo Rollouts (canary)"
  - id: po-keda
    resource: "infra/helmfile/cell.helmfile.yaml.gotmpl#L146-L149"
    note: "KEDA (event-driven autoscaling)"
  - id: po-cwts
    resource: "infra/helmfile/build.helmfile.yaml.gotmpl#L81-L83"
    note: "loopzero-build-cwts (ClusterWorkflowTemplates)"
  - id: po-read
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L356"
    note: "ArgoCD L3 syncs tenant Day-2 state FROM the Gitea repositories the API pushes to — the two halves of the GitOps loop"
  - id: po-sync
    resource: "api/internal/release/argocd/sync_client.go#L190"
    note: "POST /api/v1/applications/{app}/sync with {\"prune\": true} against the ArgoCD API, bearer-authenticated, preceded by a best-effort hard refresh to drop the repo-"
  - id: po-submit
    resource: "api/internal/builds/argo/buildkit.go#L43"
    note: "submits Workflow CRs through the dynamic client using the argoproj.io workflows GVR"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

Six orchestrating controllers, each on the plane where its work happens.

| Orchestrator | Plane | Role |
|---|---|---|
| `orch.argocd` | control | L3 GitOps — syncs tenant Day-2 state from Gitea, manual sync |
| `orch.argo_workflows` | build | runs the build DAG submitted per deploy |
| `orch.argo_events` | build | event sources feeding workflow submission |
| `orch.build_cwts` | build | the ClusterWorkflowTemplates the DAG instantiates |
| `orch.argo_rollouts` | cell | canary orchestration for tenant workloads |
| `orch.keda` | cell | event-driven autoscaling, including scale-to-zero |

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `orch.argocd` | `infra/helmfile/core.helmfile.yaml.gotmpl` | orchestrator | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:356-361` | Helmfile release 'argocd', chart argo/argo-cd 7.3.11, namespace argocd. Syncs tenant manifests out of Gitea into the workload cells. needs: cert-manager, traefik.  [merged from duplicate infra.argocd: argo/argo-cd 7.3.11. Layer L3 of the declared layering: tenant Day-2 GitOps syncing from Gitea. Driven by api/internal/release/argocd and operators/internal/argocd.] |
| `orch.argo_workflows` | `infra/helmfile/build.helmfile.yaml.gotmpl` | orchestrator | l2-platform | `infra/helmfile/build.helmfile.yaml.gotmpl:68-71` | Helmfile release 'argo-workflows', chart argo/argo-workflows 1.0.5, namespace argo. Runs the build DAG (clone, generate dockerfile, BuildKit, cosign sign, push).  [merged from duplicate infra.argoworkflows: argo/argo-workflows 1.0.5. Executes the build DAG that api/internal/builds/argo generates.] |
| `orch.argo_events` | `infra/helmfile/build.helmfile.yaml.gotmpl` | orchestrator | l2-platform | `infra/helmfile/build.helmfile.yaml.gotmpl:113-115` | Helmfile releases 'argo-events' + 'argo-events-bus' (in-repo wrapper charts) in the argo-events namespace on the build cluster.  [merged from duplicate infra.argoevents: Argo Events controller plus a JetStream NATS EventBus, declared as separate releases so CRDs register before the CR applies.] |
| `orch.argo_rollouts` | `infra/helmfile/cell.helmfile.yaml.gotmpl` | orchestrator | l2-platform | `infra/helmfile/cell.helmfile.yaml.gotmpl:171-173` | Helmfile release 'argo-rollouts', in-repo wrapper chart ../helm/argo-rollouts, installed on every workload cell. Orchestrates the per-plan canary step sequence.  [merged from duplicate infra.argorollouts: Runs on every cell. Orchestrates the canary steps that api/internal/manifests renders as Rollout + AnalysisTemplate.] |
| `orch.keda` | `infra/helmfile/cell.helmfile.yaml.gotmpl` | orchestrator | l2-platform | `infra/helmfile/cell.helmfile.yaml.gotmpl:146-149` | Helmfile releases 'keda' (kedacore/keda 2.16.1) and 'keda-http-addon' (kedacore/keda-add-ons-http 0.8.0) on each workload cell.  [merged from duplicate infra.keda: Event-driven autoscaling on the workload plane; pkg/enforcement gates the scale-to-zero path behind plan tier >= Pro.] |
| `orch.build_cwts` | `infra/helmfile/build.helmfile.yaml.gotmpl` | batch_job | l2-platform | `infra/helmfile/build.helmfile.yaml.gotmpl:81-83` | Helmfile release 'loopzero-build-cwts', in-repo published chart ../../charts/loopzero-build-cwts. The build pipeline itself, expressed as Argo ClusterWorkflowTemplates.  [merged from duplicate infra.buildcwts: ClusterWorkflowTemplates (loopzero-clone, loopzero-cache-save, loopzero-cache-restore) referenced by every submitted build. Its comment records that Pulumi build-bootstrap.ts used to deploy these and that after T1.1 this Helmfile release is the sole source.] |

# Relationships

- ArgoCD **reads** the tenant GitOps repositories that
  [`/components/release-delivery.md`](/components/release-delivery.md) writes — the two halves
  of the loop, neither calling the other.[^po-read]
- The API **publishes** a sync request to ArgoCD's HTTP API, preceded by a hard refresh to drop
  the repo-server's cached comparison error.[^po-sync]
- `api/internal/builds/argo` **writes** a Workflow CR into namespace `argo` on the build
  cluster and polls its phase — see
  [`/components/build-orchestration.md`](/components/build-orchestration.md).[^po-submit]
- Argo Rollouts runs the per-plan canary ladder: Starter 50→100, Pro 20→Analysis→50→100,
  Team/Enterprise 10→Analysis→30→60→100. The renderer emits `progressDeadlineSeconds: 300` on
  every Rollout so a failed canary reaches Degraded inside the e2e budget rather than at the
  600s default.
- KEDA is what makes scale-to-zero possible; asking for it on a cluster without KEDA returns
  `SCALE_TO_ZERO_UNAVAILABLE` with the remedy in the message —
  [`/flows/developer-first-app-live.md`](/flows/developer-first-app-live.md), step 12.

## Two configuration traps the code records

Argo Rollouts 1.7.x defaults its Traefik traffic-router queries to the legacy
`traefik.containo.us` API group. This platform runs Traefik v3 on `traefik.io/v1alpha1`, so
`controller.extraArgs` pins `--traefik-api-group=traefik.io` and
`--traefik-api-version=v1alpha1`.

On ArgoCD's Helm chart v7.x the notification configuration key is `notifications.notifiers`;
`services` is **silently ignored**.

# Evidence

[^po-argocd]: `infra/helmfile/core.helmfile.yaml.gotmpl:356-361`
[^po-wf]: `infra/helmfile/build.helmfile.yaml.gotmpl:68-71`
[^po-ev]: `infra/helmfile/build.helmfile.yaml.gotmpl:113-115`
[^po-ro]: `infra/helmfile/cell.helmfile.yaml.gotmpl:171-173`
[^po-keda]: `infra/helmfile/cell.helmfile.yaml.gotmpl:146-149`
[^po-cwts]: `infra/helmfile/build.helmfile.yaml.gotmpl:81-83`
[^po-read]: `infra/helmfile/core.helmfile.yaml.gotmpl:356`
[^po-sync]: `api/internal/release/argocd/sync_client.go:190`
[^po-submit]: `api/internal/builds/argo/buildkit.go:43`
