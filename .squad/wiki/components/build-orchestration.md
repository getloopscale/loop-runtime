---
type: Component
title: "Build orchestration and the build plane"
description: "Submits an Argo Workflow to the build cluster, polls it, and turns a succeeded workflow into signed images plus a manifest set."
resource: "api/internal/builds"
tags:
  - build
  - argo-workflows
  - buildkit
  - cosign
sources:
  - id: bo-svc
    resource: "api/internal/builds/build_orchestrator.go#L1"
    note: "services/build"
  - id: bo-argo
    resource: "api/internal/builds/argo/buildkit.go#L1"
    note: "buildplane/argo"
  - id: bo-adm
    resource: "api/internal/builds/admission/build.go#L1"
    note: "buildplane/admission"
  - id: bo-src
    resource: "api/internal/source/store.go#L1"
    note: "buildplane/source"
  - id: bo-shell
    resource: "api/internal/builds/shellgen/builder.go#L1"
    note: "buildplane/shellgen"
  - id: bo-contracts
    resource: "api/internal/services/deploycontracts/app_unit.go#L1"
    note: "services/deploycontracts"
  - id: bo-scaling
    resource: "api/internal/scaling/scaling_resolver.go#L1"
    note: "services/scaling"
  - id: bo-submit
    resource: "api/internal/builds/argo/buildkit.go#L43"
    note: "submits Workflow CRs through the dynamic client using the argoproj.io workflows GVR"
  - id: bo-poll
    resource: "api/internal/builds/build_reconcile_single.go#L55"
    note: "GetStatus polls the Workflow's phase from the build cluster on every reconcile tick. Note the trap the code documents: workflow.status.phase is not step truth —"
  - id: bo-cosign
    resource: "api/internal/builds/argo/cosign.go#L57-L67"
    note: "the generated Argo Workflow step runs cosign against Fulcio/Rekor"
  - id: bo-cycle-note
    resource: "api/internal/builds/argo/buildkit.go#L19"
    note: "Cartography note: collapsing buildplane/* and services/* into one node each manufactured a cycle that does not exist"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

Two cooperating halves. `api/internal/builds` is the orchestrator — it owns the
environment lock, the build row, and the reconcile loop. `api/internal/buildplane` is the
adapter onto Argo Workflows: it renders a DAG, submits it into the `argo` namespace of the
**build cluster**, and polls it back.[^bo-svc][^bo-argo]

The critical invariant is the lock boundary. The concurrent-build check runs **inside** the
`(tenant, project, environment)` advisory lock; the workflow submission runs **outside** it, so
Argo latency does not block other deploys on the same environment.

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `api.services` | `api/internal/services` | package | — | `api/internal/builds/build_orchestrator.go:1` | Directory grouping with NO Go files of its own; 8 sub-packages (build, deploycontracts, domain, placement, platform, preview, release, scaling). |
| `api.services.build` | `api/internal/builds` | package | application | `api/internal/builds/build_orchestrator.go:1` | The build orchestrator. The widest service: it imports buildplane/source, cfkv, db, db/store, events, manifests, metrics, ports, processing, scrub, sse and three sibling services. |
| `api.services.deploycontracts` | `api/internal/services/deploycontracts` | package | domain | `api/internal/services/deploycontracts/app_unit.go:1` | The shared deploy contract types. The hub of the services graph: SIX of the eight services sub-packages import it (build, domain, platform, preview, release, scaling) and so does buildplane/argo. It imports only db, manifests and ports. |
| `api.services.scaling` | `api/internal/scaling` | package | application | `api/internal/scaling/scaling_resolver.go:1` | Scaling resolution; imports db/store and deploycontracts only. |
| `api.buildplane` | `api/internal/buildplane` | package | — | `api/internal/builds/argo/buildkit.go:1` | Directory grouping with NO Go files of its own; 4 sub-packages: argo (workflow specs), source (bundle upload/validation), admission (capacity gate), shellgen. |
| `api.buildplane.admission` | `api/internal/builds/admission` | package | application | `api/internal/builds/admission/build.go:1` | Per-cell concurrent build ceiling and per-tenant build quota, both returning HTTP 429 with a Retry-After hint. Gated by LOOPZERO_ADMISSION_CONTROL, which its own doc says defaults to false — when off, Check always admits. |
| `api.buildplane.argo` | `api/internal/builds/argo` | package | infrastructure | `api/internal/builds/argo/buildkit.go:1` | 15 files / 8,212 lines. The only api package importing the third-party github.com/getloopscale/loopzero-forge SDK. Also owns canonical_dockerfile.go, which replaced loopzero-packs with two platform-controlled multi-stage Dockerfiles for a fixed python+node stack. |
| `api.buildplane.shellgen` | `api/internal/builds/shellgen` | package | foundation | `api/internal/builds/shellgen/builder.go:1` | Shell fragment generation for workflow steps. Zero imports; one consumer (buildplane/argo). |
| `api.buildplane.source` | `api/internal/source` | package | infrastructure | `api/internal/source/store.go:1` | Bundle upload store over Gitea, safe tar extraction, and the services.json validators that form the LoopZeroKit/LoopZeroCloud seam. |

## Measured size

None

# Relationships

- `api.buildplane.argo` **writes** a Workflow CR through the dynamic client into namespace
  `argo` on the build cluster, retrying only on 5xx-class Kubernetes errors.[^bo-submit]
- It then **reads** the workflow phase back on every reconcile tick — and the code documents the
  trap: `workflow.status.phase` is **not step truth**. An `onExit` handler can report
  `Succeeded` over a failed `clone-repo` step, which is how a build reaches the manifest stage
  with no image digest.[^bo-poll]
- The generated workflow **calls** Sigstore (Fulcio/Rekor) through a `cosign` step; the digest
  that step produces is what the manifest renderer pins images by, rather than a
  tag.[^bo-cosign]
- `api.buildplane.admission` is the per-cell admission gate consulted before a build is
  accepted, and it **fails open** on a query error — see step 8 of
  [`/flows/deploy-container-app.md`](/flows/deploy-container-app.md).
- `api.services.scaling` resolves `app_scaling_config` and applies the plan replica cap; asking
  for `replicas` and `autoscaling` together is refused as a typed
  `ErrScalingAmbiguous`.[^bo-scaling]
- `api.buildplane.source` stores uploaded source bundles through the shared Gitea repo manager
  documented in [`/components/release-delivery.md`](/components/release-delivery.md).
- Build state is the `Build` machine in
  [`/entities/state-machine-build.md`](/entities/state-machine-build.md); the row it writes is
  [`/entities/database-schema.md`](/entities/database-schema.md) `builds`.

# A cartography note, not a code defect

An earlier structural pass collapsed `buildplane/*` and `services/*` into one node each. That
**manufactured a dependency cycle that does not exist** in the package graph. The map keeps the
subpackages as distinct nodes for that reason, and the collapsed reading is recorded as a note
so a later pass does not re-derive the phantom cycle.[^bo-cycle-note]

# Evidence

[^bo-svc]: `api/internal/builds/build_orchestrator.go:1`
[^bo-argo]: `api/internal/builds/argo/buildkit.go:1`
[^bo-adm]: `api/internal/builds/admission/build.go:1`
[^bo-src]: `api/internal/source/store.go:1`
[^bo-shell]: `api/internal/builds/shellgen/builder.go:1`
[^bo-contracts]: `api/internal/services/deploycontracts/app_unit.go:1`
[^bo-scaling]: `api/internal/scaling/scaling_resolver.go:1`
[^bo-submit]: `api/internal/builds/argo/buildkit.go:43`
[^bo-poll]: `api/internal/builds/build_reconcile_single.go:55`
[^bo-cosign]: `api/internal/builds/argo/cosign.go:57-67`
[^bo-cycle-note]: `api/internal/builds/argo/buildkit.go:19`
